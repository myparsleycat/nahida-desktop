import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/xxmi/gimi')({
  component: RouteComponent,
})

function RouteComponent() {
  return <div>Hello "/xxmi/gimi"!</div>
}
