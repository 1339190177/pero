import { writeFileSync } from 'node:fs';
if (process.env.FIXTURE_ENV_REPORT) {
  const forbidden = ['PERO_OPERATOR_TOKEN', 'PERO_WORKER_TOKEN', 'PERO_DATABASE_URL', 'PERO_TEST_DATABASE_URL'];
  writeFileSync(process.env.FIXTURE_ENV_REPORT, JSON.stringify({ leaked: forbidden.filter(key => process.env[key]) }));
}
await import('./acp-agent.mjs');
