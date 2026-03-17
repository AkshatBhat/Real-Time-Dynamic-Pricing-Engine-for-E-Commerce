import { NextResponse } from "next/server";
import { proxyJSONRequest } from "@/lib/server-proxy";

export async function POST(request: Request) {
  try {
    const payload = await request.json();
    return proxyJSONRequest("POST", "/override", {
      body: payload,
      incomingRequest: request,
      requireOperatorKey: true,
    });
  } catch {
    return NextResponse.json({ error: "invalid request body" }, { status: 400 });
  }
}
