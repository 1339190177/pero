import React, { useCallback, useEffect, useRef, useState } from 'react';
import * as AppSDK from '@pero/app-sdk';
import { downloadFile, useAppAgent, useAppApi, useAppI18n, useAppInfo } from '@pero/app-sdk';
import { Button, Checkbox, EmptyState, Feedback, Field, Icon, Input, Select, Tabs, TabsContent, TabsList, TabsTrigger, Textarea, Toolbar } from '@pero/app-sdk/ui';
import { messages } from './i18n';
import { ImportPanel, PaperList } from './library';
import { EvidencePanel, Experiments, Reader, ReportView, type Citation } from './documents';
import { useResearch } from './use-research';
import type { EvidenceLocation, Library, Paper } from './types';
import './styles.css';

// App chat is optional; research sessions use the Host Agent API independently.
const useHostChat = Reflect.get(AppSDK, 'useAppChat') as (() => { open(): void; compose(text: string): void }) | undefined;

export default function PaperAgent() {
  const api = useAppApi(), agent = useAppAgent(), chat = useHostChat?.(), info = useAppInfo(), { t } = useAppI18n(messages);
  const [library, setLibrary] = useState<Library>({ papers: [], intents: [], reports: [] });
  const [loading, setLoading] = useState(true), [error, setError] = useState('');
  const [selected, setSelected] = useState<string[]>([]), [reading, setReading] = useState<Paper | null>(null), [readingBusy, setReadingBusy] = useState(false);
  const [libraryView, setLibraryView] = useState('papers'), [view, setView] = useState('source'), [adding, setAdding] = useState(false), [showResearch, setShowResearch] = useState(true);
  const [question, setQuestion] = useState(''), [verifyCode, setVerifyCode] = useState(false), [verificationPlan, setVerificationPlan] = useState(''), [agentId, setAgentId] = useState('');
  const [citation, setCitation] = useState<Citation | null>(null), [evidence, setEvidence] = useState<EvidenceLocation | null>(null), [evidenceBusy, setEvidenceBusy] = useState(false);
  const mounted = useRef(true), active = useRef(info.active), readVersion = useRef(0), libraryVersion = useRef(0), evidenceVersion = useRef(0);
  const citationTrigger = useRef<HTMLElement | null>(null), addTrigger = useRef<HTMLDivElement>(null);
  active.current = info.active;
  const onError = useCallback((value: unknown) => setError(value instanceof Error ? value.message : String(value)), []);
  const refresh = useCallback(async () => {
    const version = ++libraryVersion.current, result = await api.get<Library>('/api/library');
    if (mounted.current && version === libraryVersion.current) setLibrary(result);
  }, [api]);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; readVersion.current++; evidenceVersion.current++; libraryVersion.current++; }; }, []);
  useEffect(() => {
    if (!info.active) return;
    let alive = true; setLoading(true);
    refresh().catch(error => { if (alive) onError(error); }).finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
  }, [info.active, refresh, onError]);
  useEffect(() => {
    if (!agentId || !agent.agents.some(item => item.available && item.id === agentId)) setAgentId(agent.agents.find(item => item.available)?.id ?? '');
  }, [agent.agents, agentId]);
  const task = useResearch(api, agent, info.active, refresh, onError, t);
  const locked = task.working || task.busy || task.pending;
  useEffect(() => {
    if (!task.intent) return;
    setQuestion(task.intent.question); setSelected(task.intent.papers.map(paper => paper.id));
    setVerifyCode(task.intent.verifyCode); setVerificationPlan(task.intent.verificationPlan); setCitation(null); evidenceVersion.current++;
  }, [task.intent?.id]);
  useEffect(() => { if (task.report) { setAdding(false); setView('report'); } }, [task.report?.id]);

  async function read(paper: Paper) {
    const version = ++readVersion.current; setReadingBusy(true); setReading(null); setCitation(null); setAdding(false); setView('source');
    try {
      const full = paper.content ? paper : await api.get<Paper>(`/api/papers/${paper.id}`);
      if (mounted.current && version === readVersion.current) { setReading(full); setError(''); }
    } catch (error) { if (mounted.current && version === readVersion.current) onError(error); }
    finally { if (mounted.current && version === readVersion.current) setReadingBusy(false); }
  }
  async function added(paper: Paper, focus: boolean | (() => boolean)) {
    await refresh();
    if (!mounted.current || !active.current || !(typeof focus === 'function' ? focus() : focus)) return;
    if (!locked) setSelected(prior => prior.includes(paper.id) || prior.length >= 5 ? prior : [...prior, paper.id]);
    await read(paper);
  }
  async function example() {
    try {
      const input = await api.get<Record<string, unknown>>('/api/example');
      const paper = await api.post<Paper>('/api/papers/import', input);
      await added(paper, true);
    } catch (error) { onError(error); }
  }
  async function full() {
    if (!reading || readingBusy) return;
    const source = reading, version = ++readVersion.current; setReadingBusy(true);
    try {
      const paper = await api.post<Paper>('/api/papers', { source: source.source, id: source.sourceId, full: true });
      await added(paper, () => version === readVersion.current);
    } catch (error) { if (mounted.current && version === readVersion.current) onError(error); }
    finally { if (mounted.current && version === readVersion.current) setReadingBusy(false); }
  }
  async function cite(value: Citation) {
    citationTrigger.current = document.activeElement as HTMLElement;
    setCitation(value); setEvidence(null); setView('report'); const version = ++evidenceVersion.current;
    if (!value.quote || !task.intent) { setEvidenceBusy(false); return; }
    setEvidenceBusy(true);
    try {
      const result = await api.post<EvidenceLocation>('/api/evidence', { intentId: task.intent.id, paperId: value.paperId, quote: value.quote });
      if (mounted.current && version === evidenceVersion.current) setEvidence(result);
    } catch (error) { if (mounted.current && version === evidenceVersion.current) onError(error); }
    finally { if (mounted.current && version === evidenceVersion.current) setEvidenceBusy(false); }
  }
  function closeEvidence() {
    evidenceVersion.current++; setCitation(null);
    requestAnimationFrame(() => { if (citationTrigger.current?.isConnected) citationTrigger.current.focus({ preventScroll: true }); });
  }
  async function exportReport(format: 'markdown' | 'json') {
    if (!task.report) return;
    try {
      const result = await api.get<{ filename: string; content: string; mime: string }>(`/api/reports/${task.report.id}/export?format=${format}`);
      downloadFile(result.filename, result.content, result.mime);
    } catch (error) { onError(error); }
  }
  function excerpt(text: string) {
    if (!text) { onError(new Error(t('noSelection'))); return; }
    setQuestion(prior => `${prior}${prior ? '\n\n' : ''}> ${text}`); setShowResearch(true);
    requestAnimationFrame(() => document.getElementById('paper-question')?.focus());
  }
  function discuss(text: string) {
    if (!text || !reading) { onError(new Error(t('noSelection'))); return; }
    chat?.compose(`${t('discussPrompt')}\n\n${reading.title}\n${reading.url}\n${reading.contentDigest}\n\n> ${text}`);
  }
  function begin(execute: boolean) {
    setError('');
    void task.begin({ question, paperIds: selected, verifyCode, verificationPlan }, agentId, execute);
  }
  const frozen = task.intent, citationPaper = frozen?.papers.find(paper => paper.id === citation?.paperId);
  const canPrepare = !!selected.length && !!question.trim() && (!verifyCode || !!verificationPlan.trim()) && !locked;
  const runStatus = task.status === 'needs_input' ? t('needsInput') : task.status === 'running' ? t('running') : task.status === 'completed' ? t('completed') : task.status === 'cancelled' ? t('cancelled') : task.status === 'cancelling' ? t('cancelling') : task.status === 'failed' ? t('failed') : '';
  return <div className="pa-workbench" data-research={showResearch}>
    <header className="pa-workbench-toolbar">
      <div className="pa-toolbar-title">{reading?.title || t('papers')}</div>
      <div className="pa-actions" ref={addTrigger}><Button onClick={() => setAdding(true)}><Icon name="plus"/>{t('add')}</Button><Button variant="ghost" aria-pressed={showResearch} onClick={() => setShowResearch(value => !value)}>{t('showResearch')}</Button></div>
    </header>
    {error && <div className="pa-global-error"><Feedback kind="error"><span>{error}</span><Button variant="ghost" onClick={() => setError('')}>{t('dismiss')}</Button></Feedback></div>}
    <div className="pa-layout">
      <aside className="pa-library" aria-label={t('papers')}>
        <Tabs value={libraryView} onValueChange={setLibraryView}>
          <TabsList><TabsTrigger value="papers">{t('papers')} <span className="pa-count">{library.papers.length}</span></TabsTrigger><TabsTrigger value="reports">{t('reports')} <span className="pa-count">{library.reports.length}</span></TabsTrigger></TabsList>
          <TabsContent value="papers" forceMount>
            {loading ? <p role="status">{t('loading')}</p> : <PaperList papers={library.papers} selected={selected} readingId={reading?.id ?? ''} locked={locked} onRead={paper => void read(paper)} onSelect={(id, checked) => setSelected(prior => checked ? [...prior, id] : prior.filter(item => item !== id))} t={t}/>}
          </TabsContent>
          <TabsContent value="reports" forceMount><ul className="pa-report-list">{library.reports.map(report => <li key={report.id}><Button variant="ghost" disabled={locked} onClick={() => { setError(''); void task.openReport(report.id); }}>{report.title}</Button></li>)}</ul>{!library.reports.length && <p className="pa-muted">{t('noReport')}</p>}</TabsContent>
        </Tabs>
        <footer className="pa-library-footer"><small>{t('selected')} {selected.length}/5</small><Button variant="ghost" onClick={() => { setError(''); void refresh().catch(onError); }}><Icon name="refresh"/>{t('refresh')}</Button></footer>
      </aside>
      <main className="pa-stage">
        <div className="pa-import-mount" hidden={!adding}><ImportPanel api={api} t={t} active={adding && info.active} onAdded={added} onError={onError} onClose={() => { setAdding(false); addTrigger.current?.querySelector('button')?.focus(); }}/></div>
        <div className="pa-stage-content" hidden={adding}>{!library.papers.length && !reading && !loading ? <section className="pa-start"><span className="pa-start-number">01</span><h1>{t('emptyLibrary')}</h1><p>{t('emptyLibraryCopy')}</p><div className="pa-actions"><Button variant="primary" onClick={() => setAdding(true)}>{t('add')}</Button><Button onClick={() => void example()}>{t('example')}</Button></div><small>{t('exampleCopy')}</small></section>
          : <Tabs value={view} onValueChange={value => { setView(value); closeEvidence(); }} className="pa-stage-tabs">
            <div className="pa-stage-toolbar"><TabsList><TabsTrigger value="source">{t('reader')}</TabsTrigger><TabsTrigger value="report">{t('report')}</TabsTrigger><TabsTrigger value="experiments">{t('experiments')}</TabsTrigger></TabsList>
              {task.report && view === 'report' && <div className="pa-actions pa-export"><Button variant="ghost" onClick={() => void exportReport('markdown')}>{t('exportMarkdown')}</Button><Button variant="ghost" onClick={() => void exportReport('json')}>{t('exportJson')}</Button><Button variant="ghost" onClick={() => window.print()}>{t('print')}</Button></div>}
            </div>
            <TabsContent value="source" forceMount><div className="pa-document-scroll" aria-busy={readingBusy}>{readingBusy && <p className="pa-loading" role="status">{t('loading')}</p>}<Reader paper={reading} busy={readingBusy} onFull={() => void full()} onExcerpt={excerpt} onDiscuss={chat ? discuss : undefined} t={t}/></div></TabsContent>
            <TabsContent value="report" forceMount><div className="pa-report-layout" data-evidence={!!citation}><div className="pa-document-scroll"><ReportView report={task.report} intent={task.intent} onCite={value => void cite(value)} t={t}/></div><EvidencePanel citation={citation} location={evidence} paper={citationPaper} busy={evidenceBusy} onClose={closeEvidence} onRead={() => { if (citationPaper) void read(citationPaper); }} t={t}/></div></TabsContent>
            <TabsContent value="experiments" forceMount><div className="pa-document-scroll"><Experiments report={task.report} intent={task.intent} onCite={value => void cite(value)} t={t}/></div></TabsContent>
          </Tabs>}</div>
      </main>
      {showResearch && <aside className="pa-research" aria-label={t('research')}>
        <Toolbar actions={<Button variant="ghost" disabled={locked} onClick={() => { task.reset(); setQuestion(''); }}>{t('newResearch')}</Button>}><h2>{t('research')}</h2></Toolbar>
        <div className="pa-research-inputs"><span>{t('selected')} {selected.length}/5</span><ol>{selected.map(id => <li key={id}>{library.papers.find(paper => paper.id === id)?.title ?? frozen?.papers.find(paper => paper.id === id)?.title ?? id}</li>)}</ol></div>
        <label className="pa-question-label" htmlFor="paper-question">{t('question')}</label><Textarea id="paper-question" rows={5} value={question} maxLength={2000} disabled={locked} onChange={event => setQuestion(event.target.value)} placeholder={t('questionPlaceholder')}/>
        <label className="pa-checkbox-label"><Checkbox checked={verifyCode} disabled={locked} onCheckedChange={setVerifyCode}/>{t('verifyCode')}</label>
        {verifyCode && <Field label={t('boundary')}>{props => <Textarea {...props} rows={4} maxLength={4000} disabled={locked} value={verificationPlan} onChange={event => setVerificationPlan(event.target.value)} placeholder={t('boundaryPlaceholder')}/>}</Field>}
        <Select label={t('agent')} value={agentId} onChange={setAgentId} disabled={locked || !agent.available} options={agent.agents.filter(item => item.available).length ? agent.agents.filter(item => item.available).map(item => ({ value: item.id, label: item.name })) : [{ value: '', label: t('noAgentChoice') }]}/>
        {agent.workspace && <div className="pa-workspace-path"><small>{t('workspace')}</small><span title={agent.workspace}>{agent.workspace}</span></div>}
        {!agent.available && <p className="pa-muted">{t('noAgent')} <Button variant="ghost" onClick={() => void agent.refresh().catch(onError)}>{t('refresh')}</Button></p>}
        <div className="pa-run-actions">{task.pending ? <Button variant="primary" disabled={task.busy} onClick={() => begin(true)}>{t('retryStart')}</Button> : task.working ? <Button disabled={task.busy || task.status === 'cancelling'} onClick={() => void task.cancel()}>{task.status === 'cancelling' ? t('cancelling') : t('cancel')}</Button> : <Button variant="primary" disabled={!canPrepare || !agent.available || !agentId} onClick={() => begin(true)}><Icon name="play"/>{task.busy ? t('preparing') : t('start')}</Button>}
          {!task.working && !task.pending && <Button disabled={!canPrepare} onClick={() => begin(false)}>{t('prepare')}</Button>}
        </div>
        {task.status === 'failed' && task.raw && <Feedback kind="error">{task.raw}</Feedback>}
        {!!runStatus && <p className="pa-run-status" role="status">{runStatus}{task.status === 'needs_input' && chat && <Button onClick={() => chat.open()}>{t('openChat')}</Button>}{task.status === 'failed' && task.sessionId && <Button onClick={task.retryPoll}>{t('retry')}</Button>}</p>}
        {task.sessionId && <details className="pa-small-details"><summary>{t('session')}</summary><code>{task.sessionId}</code></details>}
        {frozen && <div className="pa-frozen"><div className="pa-kicker">{t('prepared')}</div><small>{frozen.id.slice(0, 16)}</small><Button variant="ghost" onClick={() => downloadFile('research-prompt.txt', frozen.prompt, 'text/plain;charset=utf-8')}>{t('prompt')}</Button>
          <details className="pa-output" open={task.repair}><summary>{t('importReport')}</summary>{task.repair && <p role="status">{t('invalidReport')}</p>}<Field label={t('rawOutput')}>{props => <Textarea {...props} rows={8} value={task.raw} disabled={task.working || task.busy} maxLength={500000} onChange={event => task.setRaw(event.target.value)} placeholder={t('reportPlaceholder')}/>}</Field><Button disabled={task.working || task.busy || !task.raw.trim()} onClick={() => { setError(''); void task.saveRaw(); }}>{t('saveReport')}</Button></details>
        </div>}
        {!!library.intents.length && <details className="pa-saved-inputs"><summary>{t('savedInputs')}</summary><Select label={t('savedInputs')} value={frozen?.id ?? ''} disabled={locked} onChange={id => { if (id) void task.openIntent(id); }} options={[{ value: '', label: t('chooseInput') }, ...library.intents.map(intent => ({ value: intent.id, label: intent.question }))]}/></details>}
      </aside>}
    </div>
  </div>;
}
