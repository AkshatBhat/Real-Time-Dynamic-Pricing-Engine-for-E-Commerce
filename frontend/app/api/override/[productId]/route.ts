import { proxyJSONRequest } from "@/lib/server-proxy";

export async function DELETE(request: Request, context: { params: Promise<{ productId: string }> }) {
  const { productId } = await context.params;
  return proxyJSONRequest("DELETE", `/override/${encodeURIComponent(productId)}`, {
    incomingRequest: request,
    requireOperatorKey: true,
  });
}
