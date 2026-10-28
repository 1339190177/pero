# Session-backed todos

`Mount(ctx, h, Options{})` explicitly registers Point's `todo_write` tool through
the existing tool registry. Each call replaces the whole list and appends a
`todo/write` session event. `Current(session)` reconstructs the list; the next
`turn/start` clears the previous standing plan. Default normalization allows at
most one in-progress item. No separate task database, background job, or product
service is added. Source and tests retain the upstream implementation apart from
module import relocation; see SOURCE.json.

This is task-list tracking, not plan-mode authorization. Point's `exit_plan_mode`
is not included: it requires the separately excluded user-question service and
review/approval integration.
