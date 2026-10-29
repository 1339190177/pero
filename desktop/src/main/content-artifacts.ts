import { createHash } from 'node:crypto';
import { mkdir, writeFile, open, readFile, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { JsonSource, ContentRead, OutputSelection } from './content-source.ts';

/** Host-owned result files, scoped to the actual calling conversation. */
export class ContentArtifacts {
  private readonly root: string;
  constructor(root: string) {
    this.root = root;
  }
  private directory(thread: string) {
    return join(this.root, createHash('sha256').update(thread).digest('hex'));
  }
  async select(thread: string, value: unknown): Promise<string> {
    const selection = new OutputSelection(JsonSource.fromValue(value));
    if (!selection.needsFile) return selection.source.text;
    const id = createHash('sha256').update(selection.source.text).digest('hex');
    const directory = this.directory(thread);
    await mkdir(directory, { recursive: true });
    await writeFile(join(directory, `${id}.json`), selection.source.text, {
      mode: 0o600,
    });
    return selection.reference(id);
  }
  async read(thread: string, request: ContentRead) {
    const path = join(this.directory(thread), `${request.id}.json`);
    const file = await open(path, 'r');
    try {
      const { size } = await file.stat();
      const selection =
        request.selection.kind === 'pointer'
          ? JsonSource.stored(await readFile(path, 'utf8')).pointer(
              request.selection.pointer,
            )
          : {
              start: request.selection.start,
              end: request.selection.end ?? size,
            };
      if (selection.start > size || selection.end > size)
        throw new Error('The byte range exceeds the artifact');
      const buffer = Buffer.alloc(
        Math.min(request.limit, selection.end - selection.start),
      );
      const { bytesRead } = await file.read(
        buffer,
        0,
        buffer.length,
        selection.start,
      );
      let start = 0,
        end = bytesRead;
      // Report actual UTF-8 boundaries, allowing the client to resume exactly.
      while (start < end && (buffer[start] & 0xc0) === 0x80) start++;
      {
        let boundary = end - 1;
        while (boundary >= start && (buffer[boundary] & 0xc0) === 0x80)
          boundary--;
        if (boundary >= start) {
          const c = buffer[boundary],
            width = c < 0x80 ? 1 : c < 0xe0 ? 2 : c < 0xf0 ? 3 : 4;
          if (boundary + width > end) {
            if (selection.start + end === selection.end)
              throw new Error(
                'endByte splits a UTF-8 character; use a JSON Pointer or a complete byte boundary',
              );
            end = boundary;
          }
        }
      }
      const endByte = selection.start + end;
      return {
        artifact: {
          id: request.id,
          bytes: size,
          mediaType: 'application/json',
        },
        startByte: selection.start + start,
        endByte,
        selectionEndByte: selection.end,
        text: buffer.subarray(start, end).toString('utf8'),
        truncated: endByte < selection.end,
        nextRead:
          endByte < selection.end
            ? {
                id: request.id,
                startByte: endByte,
                endByte: selection.end,
                maxBytes: request.limit,
              }
            : null,
      };
    } finally {
      await file.close();
    }
  }
  async forget(thread: string) {
    await rm(this.directory(thread), { recursive: true, force: true });
  }
}
