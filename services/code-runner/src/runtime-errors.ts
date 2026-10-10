import {json} from "./contract";

const NO_CONTAINER = "There is no container instance that can be provided to this Durable Object";

function failure(message: string): Response {
  const capacity = message.includes(NO_CONTAINER);
  return json({error: capacity ? "capacity_limited" : "runner_unavailable"}, capacity ? 429 : 503);
}

async function boundedError(response: Response): Promise<string> {
  const reader = response.body?.getReader();
  if (!reader) return "";
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    for (;;) {
      const {done, value} = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > 1024) return "";
      chunks.push(value);
    }
    const bytes = new Uint8Array(size);
    let offset = 0;
    for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length; }
    return new TextDecoder().decode(bytes);
  } finally { await reader.cancel().catch(() => {}); }
}

// Cloudflare can refuse a container-backed actor before its fetch handler runs.
// Map that definite capacity rejection to the gateway's refundable 429 contract.
// Unknown failures remain uncertain; raw platform messages never reach the app.
export async function forwardJob(job: {fetch(request: Request): Promise<Response>}, request: Request): Promise<Response> {
  try {
    const response = await job.fetch(request);
    if (response.status < 500) return response;
    return failure(await boundedError(response));
  } catch (error) {
    return failure(error instanceof Error ? error.message : "");
  }
}
