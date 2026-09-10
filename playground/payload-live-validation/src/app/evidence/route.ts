import { events, reset } from "../../trace";
// Disposable loopback-only investigation server; never included in Ridu or deployed.
export async function GET() {
	return Response.json(events());
}
export async function DELETE() {
	reset();
	return Response.json({ reset: true });
}
