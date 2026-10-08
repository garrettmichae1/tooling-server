import { Container } from "@cloudflare/containers";
import { serve, type HostBindings } from "./gateway.js";

interface Env extends HostBindings {
  STUDY_CONTAINER: DurableObjectNamespace<StudyContainer>;
}

export class StudyContainer extends Container<Env> {
  defaultPort = 8080;
  sleepAfter = "10m";
  enableInternet = false;
  pingEndpoint = "container/health";

  constructor(ctx: DurableObjectState<{}>, env: Env) {
    super(ctx, env);
    // This is the only binding passed into Go. No Apple, model, or payment keys.
    this.envVars = { FIGURE_TOKEN: env.FIGURE_TOKEN ?? "" };
  }

  override onError(): never { throw Error("study_container_unavailable"); }
  override async onActivityExpired(): Promise<void> { await this.stop(); }

  override async fetch(request: Request): Promise<Response> {
    try {
      await this.startAndWaitForPorts({ ports: 8080, cancellationOptions: {
        abort: request.signal, instanceGetTimeoutMS: 12_000, portReadyTimeoutMS: 2_000,
      } });
      this.renewActivityTimeout();
      // Use the private port directly so SDK proxy diagnostics cannot include
      // supplied content or arbitrary upstream error text in logs/responses.
      return await this.ctx.container!.getTcpPort(8080).fetch(request);
    } catch {
      return new Response('{"error":"study_container_unavailable"}', { status: 503, headers: { "Content-Type": "application/json" } });
    }
  }
}

export default { fetch: serve } satisfies ExportedHandler<Env>;
