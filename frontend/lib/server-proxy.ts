import { NextResponse } from "next/server";

const backendBaseURL = (process.env.BACKEND_BASE_URL ?? "http://localhost:8080").replace(/\/+$/, "");
const defaultBackendAPIKeyHeader = "X-API-Key";
const backendAPIKeyHeader = (process.env.BACKEND_API_KEY_HEADER ?? defaultBackendAPIKeyHeader).trim() || defaultBackendAPIKeyHeader;
const operatorKeyHeader = "X-Operator-Key";

function backendURL(path: string): string {
  const normalizedPath = path.startsWith("/") ? path : `/${path}`;
  return `${backendBaseURL}${normalizedPath}`;
}

async function readPayload(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text) {
    return {};
  }

  try {
    return JSON.parse(text);
  } catch {
    return { error: text };
  }
}

type ProxyRequestOptions = {
  body?: unknown;
  incomingRequest?: Request;
  requireOperatorKey?: boolean;
};

export async function proxyJSONRequest(method: string, path: string, options?: ProxyRequestOptions): Promise<NextResponse> {
  try {
    const headers: Record<string, string> = {
      Accept: "application/json",
    };

    const requestInit: RequestInit = {
      method,
      headers,
      cache: "no-store",
    };

    if (options?.requireOperatorKey) {
      const operatorKey = options.incomingRequest?.headers.get(operatorKeyHeader)?.trim() ?? "";
      if (operatorKey === "") {
        return NextResponse.json({ error: "missing operator api key" }, { status: 401 });
      }
      headers[backendAPIKeyHeader] = operatorKey;
    }

    if (options?.body !== undefined) {
      headers["Content-Type"] = "application/json";
      requestInit.body = JSON.stringify(options.body);
    }

    const response = await fetch(backendURL(path), requestInit);
    const payload = await readPayload(response);

    return NextResponse.json(payload, { status: response.status });
  } catch {
    return NextResponse.json({ error: "backend unavailable" }, { status: 502 });
  }
}
