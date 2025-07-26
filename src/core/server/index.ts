import { Elysia } from 'elysia'
import { node } from '@elysiajs/node'
import { cors } from '@elysiajs/cors';
import live from './live';
import gb from './gb';

// @ts-ignore
const app = new Elysia({ adapter: node() })
  .use(cors())
  .get('/ping', () => 'pong')
  .group('/live', (app) => app.use(live))
  .group('/gb', (app) => app.use(gb))

export default app;