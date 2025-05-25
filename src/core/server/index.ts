import { Elysia } from 'elysia'
import { node } from '@elysiajs/node'
import { cors } from '@elysiajs/cors';
import live from './live';

const app = new Elysia({ adapter: node() })
  .use(cors())
  .get('/ping', () => 'pong')
  .group('/live', (app) => app.use(live))

export default app;