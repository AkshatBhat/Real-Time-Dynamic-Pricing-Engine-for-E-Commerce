import { proxyJSONRequest } from "@/lib/server-proxy";

export async function GET(_: Request, context: { params: Promise<{ productId: string }> }) {
  const { productId } = await context.params;
  return proxyJSONRequest("GET", `/price/${encodeURIComponent(productId)}`);
}
