package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

func communityFixture(t *testing.T) (*Store, *Task, Worker, Worker) {
	t.Helper()
	s, _ := integrationStore(t)
	ctx := context.Background()
	for id, login := range map[int64]string{101: "alice", 102: "bob"} {
		if err := s.CommunityUpsertUser(ctx, id, login); err != nil {
			t.Fatal(err)
		}
	}
	alice, _ := mustWorker(t, s, "p")
	bob, _ := mustWorker(t, s, "p")
	if err := s.CommunityBindWorker(ctx, alice.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.CommunityBindWorker(ctx, bob.ID, "bob"); err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(ctx, testSpec("p"), "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CommunityPublish(ctx, task.ID, "Public title", "Public requirements", "https://github.com/phodal/pero/issues/2"); err != nil {
		t.Fatal(err)
	}
	return s, task, alice, bob
}
func TestCommunityConcurrentClaimAndIndependentAcceptance(t *testing.T) {
	s, task, alice, bob := communityFixture(t)
	ctx := context.Background()
	if _, err := s.CommunityClaim(ctx, task.ID, alice.ID, 102); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign Worker: %v", err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	winner := int64(0)
	runID := ""
	worker := ""
	for _, candidate := range []struct {
		id     int64
		worker string
	}{{101, alice.ID}, {102, bob.ID}} {
		wg.Add(1)
		go func(id int64, w string) {
			defer wg.Done()
			run, err := s.CommunityClaim(ctx, task.ID, w, id)
			if err == nil {
				mu.Lock()
				defer mu.Unlock()
				if winner != 0 {
					t.Error("two contributors claimed one task")
				}
				winner, runID, worker = id, run, w
			} else if !errors.Is(err, ErrConflict) {
				t.Errorf("claim: %v", err)
			}
		}(candidate.id, candidate.worker)
	}
	wg.Wait()
	if winner == 0 {
		t.Fatal("no claim winner")
	}
	if err := s.CommunityAccept(ctx, task.ID, runID, "https://example.com/report", "Not yet finished"); !errors.Is(err, ErrConflict) {
		t.Fatalf("queued award: %v", err)
	}
	run, err := s.Claim(ctx, worker, "instance", "claim")
	if err != nil || run == nil || run.ID != runID {
		t.Fatalf("worker claim: %+v %v", run, err)
	}
	if _, err = s.Complete(ctx, run.ID, worker, "instance", run.Generation, Completion{Outcome: "succeeded", Summary: "Agent says success"}); err != nil {
		t.Fatal(err)
	}
	ranks, err := s.CommunityLeaderboard(ctx, time.Unix(0, 0))
	if err != nil || len(ranks) != 0 {
		t.Fatalf("success manufactured points: %+v %v", ranks, err)
	}
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.CommunityAccept(ctx, task.ID, runID, "https://example.com/report", "Reviewed"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	ranks, err = s.CommunityLeaderboard(ctx, time.Unix(0, 0))
	if err != nil || len(ranks) != 1 || ranks[0].Points != 1 {
		t.Fatalf("duplicate scoring: %+v %v", ranks, err)
	}
	history, _, err := s.CommunityHistory(ctx, "", task.ID, 0)
	if err != nil || len(history) != 1 {
		t.Fatalf("history: %+v %v", history, err)
	}
	award := history[0].ID
	// Award in a previous week, revoke now: the old period must also be corrected.
	if _, err = s.pool.Exec(ctx, `UPDATE community_points SET created_at=now()-interval '10 days' WHERE id=$1`, award); err != nil {
		t.Fatal(err)
	}
	if err = s.CommunityReverse(ctx, task.ID, award, "Review withdrawn"); err != nil {
		t.Fatal(err)
	}
	if err = s.CommunityReverse(ctx, task.ID, award, "Repeated request"); err != nil {
		t.Fatal(err)
	}
	for _, since := range []time.Time{time.Unix(0, 0), time.Now().AddDate(0, 0, -15), time.Now().AddDate(0, 0, -7)} {
		ranks, err = s.CommunityLeaderboard(ctx, since)
		if err != nil || len(ranks) != 0 {
			t.Fatalf("reversal totals: %+v %v", ranks, err)
		}
	}
	history, _, err = s.CommunityHistory(ctx, "", task.ID, 0)
	if err != nil || len(history) != 2 {
		t.Fatalf("append-only reversal: %+v %v", history, err)
	}
	if err = s.CommunityAccept(ctx, task.ID, runID, "https://example.com/report", "Stale replay"); !errors.Is(err, ErrConflict) {
		t.Fatalf("replayed acceptance resurrected revoked award: %v", err)
	}
}
func TestCommunityPrivateTasksSourceAndSessionBoundaries(t *testing.T) {
	s, task, alice, _ := communityFixture(t)
	ctx := context.Background()
	private, err := s.CreateTask(ctx, testSpec("p"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommunityTask(ctx, private.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private task: %v", err)
	}
	if _, err = s.CommunityClaim(ctx, private.ID, alice.ID, 101); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private claim: %v", err)
	}
	client, _, err := s.CreateClient(ctx, "desktop", []string{"p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimTask(ctx, &client, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommunityClaim(ctx, task.ID, alice.ID, 101); !errors.Is(err, ErrConflict) {
		t.Fatalf("Desktop ownership overwritten: %v", err)
	}
	if err = s.CommunityBindWorker(ctx, alice.ID, "bob"); !errors.Is(err, ErrConflict) {
		t.Fatalf("ownership transferred: %v", err)
	}
	if err = s.CommunityCreateSession(ctx, "session-secret", "csrf", 101, false); err != nil {
		t.Fatal(err)
	}
	session, err := s.CommunitySession(ctx, "session-secret")
	if err != nil || session.ID != 101 {
		t.Fatalf("session: %+v %v", session, err)
	}
	if _, err = s.CommunitySession(ctx, "wrong"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong token: %v", err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE community_sessions SET expires_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommunitySession(ctx, "session-secret"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired token: %v", err)
	}
}

func TestCommunityPublicSourceFactsFollowLatestProviderObservation(t *testing.T) {
	s, _, worker, _ := communityFixture(t)
	ctx := context.Background()
	config, issue := sourceFixture()
	if _, err := s.SyncGitHubIssue(ctx, config, issue); err != nil {
		t.Fatal(err)
	}
	task := sourceTask(t, s, issue)
	if err := s.CommunityPublish(ctx, task.ID, "Reviewed public title", "Reviewed public scope", issue.URL); err != nil {
		t.Fatal(err)
	}
	runID, err := s.CommunityClaim(ctx, task.ID, worker.ID, 101)
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.Claim(ctx, worker.ID, "instance", "source-run")
	if err != nil || run == nil || run.ID != runID {
		t.Fatalf("claim: %+v %v", run, err)
	}
	if _, err = s.Complete(ctx, run.ID, worker.ID, "instance", run.Generation, Completion{Outcome: "failed", Summary: "Inspect before retry"}); err != nil {
		t.Fatal(err)
	}
	oldRevision := run.Spec.Source.Revision
	check := func(state string, closed bool) {
		t.Helper()
		detail, err := s.CommunityTask(ctx, task.ID)
		if err != nil || detail.State != state || detail.SourceClosed != closed || !detail.SourceChanged {
			t.Fatalf("public detail: %+v %v", detail, err)
		}
		list, _, err := s.CommunityTasks(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range list {
			if item.ID == task.ID {
				found = true
				if item.State != detail.State || item.SourceClosed != detail.SourceClosed || item.SourceChanged != detail.SourceChanged {
					t.Fatal("public list/detail source facts disagree")
				}
			}
		}
		if !found {
			t.Fatal("published source missing")
		}
		if _, err = s.CommunityClaim(ctx, task.ID, worker.ID, 101); !errors.Is(err, ErrConflict) {
			t.Fatalf("changed source accepted another claim: %v", err)
		}
		original, err := s.Task(ctx, task.ID)
		if err != nil || original.CurrentRunID != runID || original.Source.Revision != oldRevision || original.Run.Spec.Source.Revision != oldRevision || original.Run.Spec.Source.State != "open" {
			t.Fatalf("source display changed original execution: %+v %v", original, err)
		}
	}
	issue.Body = "Updated provider goal"
	issue.UpdatedAt = "2026-10-28T00:01:00Z"
	if _, err = s.SyncGitHubIssue(ctx, config, issue); err != nil {
		t.Fatal(err)
	}
	check("source_changed", false)
	issue.State = "closed"
	issue.UpdatedAt = "2026-10-28T00:02:00Z"
	if _, err = s.SyncGitHubIssue(ctx, config, issue); err != nil {
		t.Fatal(err)
	}
	check("closed", true)
}

func TestCommunityListsImportsOnlyFromPublicRepositories(t *testing.T) {
	for _, tc := range []struct {
		name    string
		bound   bool
		private bool
		listed  int
	}{
		{"public repository", true, false, 4},
		{"private repository", true, true, 0},
		{"visibility not observed", false, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := integrationStore(t)
			ctx := context.Background()
			config, issue := sourceFixture()
			if tc.bound {
				if err := s.BindGitHubRepository(ctx, config, issue.RepositoryID, tc.private); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.SyncGitHubIssue(ctx, config, issue); err != nil {
				t.Fatal(err)
			}
			_, snapshot := roadmapFixture()
			if _, err := s.SyncRoadmap(ctx, config, snapshot); err != nil {
				t.Fatal(err)
			}
			list, _, err := s.CommunityTasks(ctx, 0)
			if err != nil || len(list) != tc.listed {
				t.Fatalf("listed %d, want %d: %v", len(list), tc.listed, err)
			}
			if tc.listed == 0 {
				return
			}
			task := sourceTask(t, s, issue)
			listed, err := s.CommunityTask(ctx, task.ID)
			if err != nil || listed.Title != issue.Title || listed.SourceURL != issue.URL {
				t.Fatalf("source listing: %+v %v", listed, err)
			}
			if err = s.CommunityPublish(ctx, task.ID, "Reviewed title", "Reviewed scope", issue.URL); err != nil {
				t.Fatal(err)
			}
			issue.Title, issue.UpdatedAt = "Edited upstream", "2026-10-28T00:01:00Z"
			if _, err = s.SyncGitHubIssue(ctx, config, issue); err != nil {
				t.Fatal(err)
			}
			if listed, err = s.CommunityTask(ctx, task.ID); err != nil || listed.Title != "Reviewed title" {
				t.Fatalf("reviewed text replaced by sync: %+v %v", listed, err)
			}
		})
	}
}

func TestPublicTaskGrouping(t *testing.T) {
	for _, test := range []struct {
		name  string
		tasks []PublicTask
		want  []string
	}{
		{name: "empty"},
		{
			name: "document title is not a functional domain",
			tasks: []PublicTask{
				{ID: "a", Project: "p", Roadmap: true, Headings: []string{"Roadmap", "Agent", "会话"}},
				{ID: "b", Project: "p", Roadmap: true, Headings: []string{"ROADMAP", "M1"}},
				{ID: "c", Project: "p", Roadmap: true, Headings: []string{"Roadmap"}},
			},
			want: []string{"p/Agent", "p/Agent/会话", "p/Agent/会话#a", "p/M1", "p/M1#b", "p/Roadmap", "p/Roadmap#c"},
		},
		{
			name: "domain feature and published task hierarchy",
			tasks: []PublicTask{
				{ID: "child", ParentID: "parent", Project: "p", Roadmap: true, Headings: []string{"Agent", "会话"}},
				{ID: "parent", Project: "p", Roadmap: true, Headings: []string{"Agent", "会话"}},
				{ID: "tools", Project: "p", Roadmap: true, Headings: []string{"Agent", "工具"}},
				{ID: "next", Project: "p", Roadmap: true, Headings: []string{"Agent", "会话"}},
			},
			want: []string{"p/Agent", "p/Agent/会话", "p/Agent/会话#parent", "p/Agent/会话#parent#child", "p/Agent/会话#next", "p/Agent/工具", "p/Agent/工具#tools"},
		},
		{
			name: "same feature labels remain in their domain and project",
			tasks: []PublicTask{
				{ID: "a", Project: "p", Roadmap: true, Headings: []string{"Agent", "管理"}},
				{ID: "b", Project: "p", Roadmap: true, Headings: []string{"App", "管理"}},
				{ID: "c", Project: "q", Roadmap: true, Headings: []string{"Agent", "管理"}},
			},
			want: []string{"p/Agent", "p/Agent/管理", "p/Agent/管理#a", "p/App", "p/App/管理", "p/App/管理#b", "q/Agent", "q/Agent/管理", "q/Agent/管理#c"},
		},
		{
			name: "unpublished or other feature parents do not create executable nodes",
			tasks: []PublicTask{
				{ID: "orphan", ParentID: "hidden", Project: "p", Roadmap: true, Headings: []string{"Agent", "会话"}},
				{ID: "child", ParentID: "parent", Project: "p", Roadmap: true, Headings: []string{"Agent", "会话"}},
				{ID: "parent", Project: "p", Roadmap: true, Headings: []string{"Agent", "工具"}},
			},
			want: []string{"p/Agent", "p/Agent/会话", "p/Agent/会话#orphan", "p/Agent/会话#child", "p/Agent/工具", "p/Agent/工具#parent"},
		},
		{
			name: "direct domain tasks and deeper headings",
			tasks: []PublicTask{
				{ID: "a", Project: "p", Roadmap: true, Headings: []string{"Agent"}},
				{ID: "b", Project: "p", Roadmap: true, Headings: []string{"Agent", "工具", "MCP"}},
			},
			want: []string{"p/Agent", "p/Agent#a", "p/Agent/工具", "p/Agent/工具/MCP", "p/Agent/工具/MCP#b"},
		},
		{
			name: "functional Roadmap roots precede ordinary sources",
			tasks: []PublicTask{
				{ID: "issue-a", Project: "p"},
				{ID: "issue-b", Project: "q"},
				{ID: "issue-c", Project: "p"},
				{ID: "agent", Project: "p", Roadmap: true, Headings: []string{"Agent"}},
				{ID: "app", Project: "p", Roadmap: true, Headings: []string{"App"}},
			},
			want: []string{"p/Agent", "p/Agent#agent", "p/App", "p/App#app", "p/其他任务", "p/其他任务#issue-a", "p/其他任务#issue-c", "q/其他任务", "q/其他任务#issue-b"},
		},
		{
			name: "ordinary and unheaded Roadmap keep fallback categories",
			tasks: []PublicTask{
				{ID: "a", Project: "p", Title: "Keep. Reviewed title", Headings: []string{"private source"}},
				{ID: "b", Project: "p", Roadmap: true},
			},
			want: []string{"p/Roadmap", "p/Roadmap#b", "p/其他任务", "p/其他任务#a"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			groups := GroupPublicTasks(test.tasks)
			var got []string
			var visitTask func(*PublicTaskTree, string)
			visitTask = func(task *PublicTaskTree, path string) {
				path += "#" + task.ID
				got = append(got, path)
				for _, child := range task.Children {
					visitTask(child, path)
				}
			}
			var visitGroup func(*PublicTaskGroup, string)
			visitGroup = func(group *PublicTaskGroup, path string) {
				path += "/" + group.Category
				got = append(got, path)
				for _, task := range group.Tasks {
					visitTask(task, path)
				}
				for _, child := range group.Children {
					visitGroup(child, path)
				}
			}
			for i := range groups {
				visitGroup(&groups[i], groups[i].Project)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("outline: %v, want %v", got, test.want)
			}
		})
	}
}

func TestPublicTaskBrief(t *testing.T) {
	for _, test := range []struct {
		name string
		task PublicTask
		want PublicTaskBrief
	}{
		{
			name: "Roadmap work scope and completion check",
			task: PublicTask{Roadmap: true, Title: "会话搜索. 为历史添加搜索", Description: "会话搜索. 按关键字搜索历史；验收：重启后仍可找到匹配会话。"},
			want: PublicTaskBrief{Scope: "按关键字搜索历史", Acceptance: "重启后仍可找到匹配会话。"},
		},
		{
			name: "short Roadmap title and legacy description",
			task: PublicTask{Roadmap: true, Title: "会话搜索", Description: "按关键字搜索历史。"},
			want: PublicTaskBrief{Scope: "按关键字搜索历史。"},
		},
		{
			name: "only exact redundant title prefix is removed",
			task: PublicTask{Roadmap: true, Title: "会话搜索", Description: "搜索. 保留原始说明；验收：显示结果。"},
			want: PublicTaskBrief{Scope: "搜索. 保留原始说明", Acceptance: "显示结果。"},
		},
		{
			name: "reviewed ordinary description remains untouched",
			task: PublicTask{Title: "会话搜索", Description: "会话搜索. 原始说明；验收：保留审核过的内容。"},
			want: PublicTaskBrief{Scope: "会话搜索. 原始说明；验收：保留审核过的内容。"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.task.Brief(); got != test.want {
				t.Fatalf("brief: %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestPublicTaskPhases(t *testing.T) {
	for _, test := range []struct {
		name string
		task PublicTask
		want PublicTaskPhase
	}{
		{"draft is available", PublicTask{State: "draft"}, PublicTaskAvailable},
		{"claim is active", PublicTask{State: "reserved"}, PublicTaskActive},
		{"queued is active", PublicTask{State: "queued"}, PublicTaskActive},
		{"running is active", PublicTask{State: "running"}, PublicTaskActive},
		{"cancel pending is active", PublicTask{State: "cancel_requested"}, PublicTaskActive},
		{"PR needs review", PublicTask{State: "submitted"}, PublicTaskReview},
		{"success needs review", PublicTask{State: "succeeded"}, PublicTaskReview},
		{"accepted overrides source closure", PublicTask{Accepted: true, SourceClosed: true, SourceChanged: true, State: "succeeded"}, PublicTaskAccepted},
		{"source closure overrides execution", PublicTask{SourceClosed: true, State: "running"}, PublicTaskClosed},
		{"closed state", PublicTask{State: "closed"}, PublicTaskClosed},
		{"closure overrides source changes", PublicTask{SourceClosed: true, SourceChanged: true, State: "draft"}, PublicTaskClosed},
		{"changed draft needs attention", PublicTask{SourceChanged: true, State: "draft"}, PublicTaskAttention},
		{"changed execution needs attention", PublicTask{SourceChanged: true, State: "running"}, PublicTaskAttention},
		{"failed", PublicTask{State: "failed"}, PublicTaskAttention},
		{"cancelled", PublicTask{State: "cancelled"}, PublicTaskAttention},
		{"reconcile", PublicTask{State: "reconcile"}, PublicTaskAttention},
		{"revoked", PublicTask{State: "revoked"}, PublicTaskAttention},
		{"changed state", PublicTask{State: "source_changed"}, PublicTaskAttention},
		{"unknown state", PublicTask{State: "future-state"}, PublicTaskAttention},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.task.Phase(); got != test.want {
				t.Fatalf("phase %q, want %q", got, test.want)
			}
		})
	}
}

func TestPublicRoadmapOutline(t *testing.T) {
	s, _ := integrationStore(t)
	ctx := context.Background()
	config, snapshot := roadmapFixture()
	snapshot.Items[0].Title = "Parent. Full parent goal"
	snapshot.Items[0].Text = "Parent. Full parent goal"
	snapshot.Items[1].Text = "Child. Full child goal"
	snapshot.Items[1].Depth = 1
	if err := s.BindGitHubRepository(ctx, config, snapshot.RepositoryID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncRoadmap(ctx, config, snapshot); err != nil {
		t.Fatal(err)
	}
	tasks, more, err := s.CommunityTasks(ctx, 0)
	if err != nil || more || len(tasks) != 3 {
		t.Fatalf("outline tasks: %+v %v", tasks, err)
	}
	parent, child := tasks[0], tasks[1]
	if parent.DisplayTitle() != "Parent" || child.ParentID != parent.ID || child.Headings[0] != "Plan" {
		t.Fatalf("outline lost: %+v", tasks)
	}
	groups := GroupPublicTasks(tasks)
	if len(groups) != 2 || len(groups[0].Tasks) != 1 || len(groups[0].Tasks[0].Children) != 1 || groups[0].Tasks[0].Children[0].ID != child.ID {
		t.Fatalf("tree hierarchy lost: %+v", groups)
	}
	snapshot.Items[0].Line, snapshot.Items[1].Line, snapshot.Items[2].Line = 50, 51, 52
	for line := 1; line < 50; line++ {
		snapshot.Items = append(snapshot.Items, RoadmapItem{ID: fmt.Sprintf("filler-%d", line), Title: fmt.Sprintf("Earlier %d", line), Text: "Earlier goal", Line: line, Headings: []string{"Plan"}})
	}
	if _, err = s.SyncRoadmap(ctx, config, snapshot); err != nil {
		t.Fatal(err)
	}
	next, more, err := s.CommunityTasks(ctx, 50)
	if err != nil || more || len(next) != 2 || next[0].ID != child.ID || next[0].ParentID != parent.ID {
		t.Fatalf("cross-page hierarchy: %+v %v", next, err)
	}
	outline, err := s.CommunityTaskOutline(ctx)
	if err != nil || len(outline) != 52 || outline[49].ID != parent.ID || outline[50].ID != child.ID || outline[50].ParentID != parent.ID || !outline[51].SourceClosed {
		t.Fatalf("complete source outline: %+v %v", outline, err)
	}
	// A complete new snapshot removes the earlier filler goals. History still
	// contains them, but they must not inflate the current panorama.
	snapshot.Items = snapshot.Items[:3]
	if _, err = s.SyncRoadmap(ctx, config, snapshot); err != nil {
		t.Fatal(err)
	}
	privateConfig, privateSnapshot := roadmapFixture()
	privateConfig.Repo = "private"
	privateSnapshot.Repository, privateSnapshot.RepositoryID = "acme/private", "private-200"
	if err = s.BindGitHubRepository(ctx, privateConfig, privateSnapshot.RepositoryID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SyncRoadmap(ctx, privateConfig, privateSnapshot); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateTask(ctx, testSpec(config.ProjectID), ""); err != nil {
		t.Fatal(err)
	}
	outline, err = s.CommunityTaskOutline(ctx)
	if err != nil || len(outline) != 3 || outline[0].ID != parent.ID || outline[1].ID != child.ID || !outline[2].SourceClosed {
		t.Fatalf("removed/private/unpublished tasks entered outline: %+v %v", outline, err)
	}
	if _, more, err = s.CommunityTasks(ctx, 0); err != nil || !more {
		t.Fatalf("legacy paginated history changed: more=%v err=%v", more, err)
	}
	if _, err = s.pool.Exec(ctx, `DELETE FROM community_tasks WHERE task_id=$1`, parent.ID); err != nil {
		t.Fatal(err)
	}
	child, err = s.CommunityTask(ctx, child.ID)
	if err != nil || child.ParentID != "" {
		t.Fatalf("unpublished parent exposed: %+v %v", child, err)
	}
	if err = s.BindGitHubRepository(ctx, config, snapshot.RepositoryID, true); err != nil {
		t.Fatal(err)
	}
	child, err = s.CommunityTask(ctx, child.ID)
	if err != nil || child.Roadmap || len(child.Headings) != 0 {
		t.Fatalf("private source context exposed: %+v %v", child, err)
	}
}
