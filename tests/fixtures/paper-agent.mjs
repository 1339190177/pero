// Deterministic ACP fixture. It reads synthetic prompt data and reports no real
// experiment, model request, file modification or scientific acceptance.
import readline from 'node:readline';
import { appendFileSync } from 'node:fs';

const capture = process.argv.find(value => value.startsWith('--capture='))?.slice('--capture='.length);
const send = value => process.stdout.write(JSON.stringify({ jsonrpc: '2.0', ...value }) + '\n');
let active;
const sessionId = 'synthetic-paper-fixture';
const contentText = block => {
  if (block?.type === 'text') return block.text ?? '';
  if (block?.type === 'resource') return block.resource?.text ?? '';
  return '';
};
const finish = stopReason => {
  if (active !== undefined) send({ id: active, result: { stopReason } });
  active = undefined;
};
readline.createInterface({ input: process.stdin }).on('line', line => {
  const { id, method, params } = JSON.parse(line);
  if (method === 'initialize') {
    send({ id, result: { protocolVersion: 1, agentCapabilities: { promptCapabilities: { embeddedContext: true } }, agentInfo: { name: 'synthetic-paper-fixture', version: '1' } } });
  } else if (method === 'session/new') {
    send({ id, result: { sessionId, configOptions: [] } });
  } else if (method === 'session/prompt') {
    active = id;
    const prompt = params.prompt.map(contentText).join('\n');
    if (capture) appendFileSync(capture, JSON.stringify({ synthetic: true, wait: prompt.includes('paper-fixture-wait'), invalid: prompt.includes('paper-fixture-invalid') }) + '\n');
    if (prompt.includes('paper-fixture-wait')) return;
    try {
      const intentId = /Frozen input: ([a-f0-9]{64})/.exec(prompt)?.[1];
      const sourceText = /\nSources:\n([\s\S]*?)\nReturn ONLY JSON:/.exec(prompt)?.[1];
      const papers = JSON.parse(sourceText);
      if (!intentId || !papers.length) throw new Error('Missing frozen research input');
      const quote = papers[0].content.split('\n').find(value => value.trim() && !value.startsWith('#'));
      const report = {
        schema: 'pero.paper-agent-report.v1', intentId, title: 'Synthetic research report',
        sections: [{ heading: 'Fixture evidence', content: 'This deterministic fixture checks source matching and report persistence. It is not a real model research result.',
          paperIds: [papers[0].id], evidence: [{ paperId: papers[0].id, quote: prompt.includes('paper-fixture-invalid') ? 'This invented quote is absent from the frozen source.' : quote }] }],
        experiments: [], limitations: 'Synthetic fixture only. No model request or scientific experiment was performed. Scientific acceptance is unverified.',
      };
      send({ method: 'session/update', params: { sessionId, update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: JSON.stringify(report) } } } });
      finish('end_turn');
    } catch (error) {
      send({ id, error: { code: -32602, message: error.message } }); active = undefined;
    }
  } else if (method === 'session/cancel') {
    finish('cancelled');
  } else if (id !== undefined) {
    send({ id, error: { code: -32601, message: 'Unsupported fixture operation' } });
  }
});
