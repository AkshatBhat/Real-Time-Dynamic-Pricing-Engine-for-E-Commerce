import { proxyJSONRequest } from "@/lib/server-proxy";

export async function GET(request: Request, context: { params: Promise<{ productId: string }> }) {
  const { productId } = await context.params;
  const url = new URL(request.url);
  const query = url.searchParams.toString();
  const suffix = query ? `?${query}` : "";

  return proxyJSONRequest("GET", `/history/${encodeURIComponent(productId)}${suffix}`, {
    incomingRequest: request,
    requireOperatorKey: true,
  });
}
