import { NextRequest } from "next/server";

/**
 * Same-origin proxy to the Go API.
 *
 * The browser only ever calls this origin, so the API needs no CORS
 * configuration and its address is never present in client JavaScript. The
 * forward runs on the Next.js server, which reaches the API over the compose
 * network (`http://api:8080`) or over localhost in local development.
 *
 * The proxy is deliberately transparent: it does not reshape bodies, translate
 * status codes, or interpret the structured error envelope. Whatever the API
 * says is what the console sees — the console is a client of exactly the same
 * contract an agent uses.
 */

const BACKEND_URL = (process.env.BACKEND_URL ?? "http://localhost:8080").replace(/\/+$/, "");

// Every proxied call must reach the API; nothing here is cacheable.
export const dynamic = "force-dynamic";

/**
 * Headers worth forwarding upstream. An allowlist rather than a passthrough:
 * `host` in particular must not be forwarded, and there is no reason for the
 * browser to be able to inject arbitrary headers into an internal call.
 */
const FORWARDED_REQUEST_HEADERS = ["content-type", "accept", "idempotency-key"];

async function proxy(request: NextRequest, path: string[]): Promise<Response> {
  const target = `${BACKEND_URL}/api/${path.map(encodeURIComponent).join("/")}${request.nextUrl.search}`;

  const headers = new Headers();
  for (const name of FORWARDED_REQUEST_HEADERS) {
    const value = request.headers.get(name);
    if (value) headers.set(name, value);
  }

  const method = request.method;
  const body = method === "GET" || method === "HEAD" ? undefined : await request.text();

  let upstream: Response;
  try {
    upstream = await fetch(target, { method, headers, body, cache: "no-store" });
  } catch (cause) {
    // The cause names an internal host and can carry connection detail, so it
    // goes to the server log and not to the browser — the same discipline the
    // Go API applies to its own 500s. The client gets a fixed message.
    console.error(`[proxy] ${method} ${target} failed:`, cause);
    return Response.json(
      // Shaped like the API's own error envelope so the console has exactly one
      // error format to render, whether the failure came from the API or from
      // not being able to reach it.
      { code: "NOT_READY", message: "the API is unreachable from the console" },
      { status: 503 },
    );
  }

  const responseHeaders = new Headers();
  const contentType = upstream.headers.get("content-type");
  if (contentType) responseHeaders.set("content-type", contentType);
  responseHeaders.set("cache-control", "no-store");

  return new Response(upstream.body, { status: upstream.status, headers: responseHeaders });
}

type Context = { params: Promise<{ path: string[] }> };

export async function GET(request: NextRequest, ctx: Context) {
  return proxy(request, (await ctx.params).path);
}

export async function POST(request: NextRequest, ctx: Context) {
  return proxy(request, (await ctx.params).path);
}

export async function PUT(request: NextRequest, ctx: Context) {
  return proxy(request, (await ctx.params).path);
}
