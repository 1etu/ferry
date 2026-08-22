import { mkdir, readFile, writeFile } from 'node:fs/promises';
import openapiTS, { astToString } from 'openapi-typescript';

const source = new URL('../../api/openapi.yaml', import.meta.url);
const target = new URL('../src/lib/api/schema.d.ts', import.meta.url);

const ast = await openapiTS(source);
const contents = astToString(ast, { formatOptions: { removeComments: true } });

const previous = await readFile(target, 'utf8').catch(() => '');
if (previous !== contents) {
  await mkdir(new URL('.', target), { recursive: true });
  await writeFile(target, contents);
}
