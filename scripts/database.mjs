import EmbeddedPostgres from 'embedded-postgres';
import { existsSync, mkdirSync } from 'node:fs';
import path from 'node:path';

// Development only, loopback only, persistent files inside this project.
const dataRoot = path.resolve('data');
mkdirSync(dataRoot, { recursive: true });
const pg = new EmbeddedPostgres({
  databaseDir: path.join(dataRoot, 'postgres'), user: 'nemi', password: 'nemi_dev_only',
  port: 55432, persistent: true, createPostgresUser: false,
  initdbFlags: ['--encoding=UTF8', '--locale=C'],
  postgresFlags: ['-h', '127.0.0.1'],
  onLog: message => { if (String(message).includes('ready to accept')) console.log('PostgreSQL ready at 127.0.0.1:55432'); },
  onError: message => console.error(String(message)),
});
if (!existsSync(path.join(dataRoot, 'postgres', 'PG_VERSION'))) await pg.initialise();
await pg.start();
const client = pg.getPgClient();
await client.connect();
for (const name of ['nemi', 'nemi_test']) {
  const existing = await client.query('SELECT 1 FROM pg_database WHERE datname=$1', [name]);
  if (!existing.rowCount) await client.query(`CREATE DATABASE ${name}`);
}
await client.end();
console.log('Development databases ready: nemi, nemi_test');
let closing = false;
async function stop() { if (closing) return; closing = true; await pg.stop(); process.exit(0); }
process.on('SIGINT', stop); process.on('SIGTERM', stop);
setInterval(() => {}, 60000);
