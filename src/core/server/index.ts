import { Elysia } from 'elysia'
import { node } from '@elysiajs/node'
import live from './live';

const app = new Elysia({ adapter: node() })
  .get('/ping', () => 'pong')
  .group('/live', (app) => app.use(live))

export default app;