export type Paper = {
  id: string; source: string; sourceId: string; title: string; authors: string[];
  url: string; pdfUrl?: string; contentKind: 'abstract' | 'report' | 'full-text';
  contentDigest: string; fetchedAt: string; content?: string;
};
export type Intent = {
  id: string; question: string; requestId: string; paperIds?: string[]; papers: Paper[];
  verifyCode: boolean; verificationPlan: string; prompt: string;
};
export type Evidence = { paperId: string; quote: string };
export type ReportSection = { heading: string; content: string; paperIds: string[]; evidence?: Evidence[] };
export type Experiment = {
  title: string; status: 'passed' | 'failed' | 'blocked' | 'not-run';
  files: { path: string; language: string; content: string }[]; commands: string[];
  paperIds: string[]; evidence?: Evidence[]; expected: string; observed: string; limitations: string;
};
export type Report = {
  id: string; intentId: string; title: string; sections?: ReportSection[];
  experiments?: Experiment[]; limitations?: string; acceptance: 'unverified';
};
export type Library = { papers: Paper[]; intents: Intent[]; reports: Report[] };
export type SearchItem = { id: string; source: string; title: string; url: string; authors?: string[]; abstract?: string };
export type EvidenceLocation = { start: number; end: number; heading?: string; before?: string; quote: string; after?: string };
export type Api = { get<T>(path: string): Promise<T>; post<T>(path: string, body: unknown): Promise<T> };
export type Agent = {
  available: boolean; agents: { id: string; name: string; available: boolean }[]; workspace: string;
  refresh(): Promise<void>;
  start(input: { requestId: string; prompt: string; agentId: string }): Promise<{ sessionId: string }>;
  events(input: { sessionId: string; after: number }): Promise<{ events: unknown[]; cursor: number; status: string; text: string }>;
  cancel(input: { sessionId: string }): Promise<void>;
};
