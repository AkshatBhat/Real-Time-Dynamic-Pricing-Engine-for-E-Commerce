"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { PRODUCT_IDS, type ProductID } from "@/lib/catalog";
import type {
  ErrorResponse,
  HistoryEntry,
  HistoryResponse,
  OverrideRequest,
  OverrideResponse,
  PriceUpdateEvent,
  PriceResponse,
} from "@/lib/types";

type ProductRowState =
  | { status: "loading" }
  | { status: "ok"; payload: PriceResponse }
  | { status: "missing"; message: string }
  | { status: "error"; message: string };

type OverrideFormState = {
  override_price: string;
  reason: string;
};

type ActionState = {
  pending: boolean;
  kind: "idle" | "success" | "error";
  message: string;
};

type HistoryState = {
  loading: boolean;
  error: string;
  rows: HistoryEntry[];
};

type FetchResult<T> = {
  ok: boolean;
  status: number;
  data: T | null;
  error: string;
};

const OPERATOR_KEY_STORAGE_KEY = "operator_api_key";
const OPERATOR_KEY_HEADER = "X-Operator-Key";
const PRICE_WS_URL = process.env.NEXT_PUBLIC_PRICE_WS_URL ?? "ws://localhost:8080/ws/prices";
const WS_RECONNECT_DELAYS_MS = [1000, 2000, 5000, 10000] as const;

type WSStatus = "connecting" | "live" | "reconnecting" | "disconnected";

const initialActionState: ActionState = {
  pending: false,
  kind: "idle",
  message: "",
};

function parseResponsePayload<T>(raw: string): T | ErrorResponse | null {
  if (!raw) {
    return null;
  }

  try {
    return JSON.parse(raw) as T | ErrorResponse;
  } catch {
    return { error: raw };
  }
}

async function requestJSON<T>(path: string, init?: RequestInit): Promise<FetchResult<T>> {
  try {
    const response = await fetch(path, {
      ...init,
      cache: "no-store",
      headers: {
        Accept: "application/json",
        ...(init?.headers ?? {}),
      },
    });

    const text = await response.text();
    const parsed = parseResponsePayload<T>(text);

    if (!response.ok) {
      const message =
        parsed && typeof parsed === "object" && "error" in parsed && typeof parsed.error === "string"
          ? parsed.error
          : "request failed";

      return {
        ok: false,
        status: response.status,
        data: null,
        error: message,
      };
    }

    return {
      ok: true,
      status: response.status,
      data: (parsed as T | null) ?? null,
      error: "",
    };
  } catch {
    return {
      ok: false,
      status: 502,
      data: null,
      error: "backend unavailable",
    };
  }
}

function formatCurrency(value: number): string {
  return `$${value.toFixed(2)}`;
}

function formatTimestamp(isoTime: string): string {
  const date = new Date(isoTime);
  return date.toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function parsePriceUpdateEvent(rawData: unknown): PriceUpdateEvent | null {
  if (typeof rawData !== "string") {
    return null;
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(rawData);
  } catch {
    return null;
  }

  if (!parsed || typeof parsed !== "object") {
    return null;
  }

  const maybe = parsed as Partial<PriceUpdateEvent>;
  if (maybe.type !== "price_update") {
    return null;
  }
  if (typeof maybe.product_id !== "string" || maybe.product_id.trim() === "") {
    return null;
  }
  if (typeof maybe.new_price !== "number") {
    return null;
  }
  if (typeof maybe.timestamp !== "number") {
    return null;
  }

  return {
    type: "price_update",
    product_id: maybe.product_id,
    new_price: maybe.new_price,
    timestamp: maybe.timestamp,
  };
}

export default function Dashboard() {
  const [operatorKey, setOperatorKey] = useState("");
  const [wsStatus, setWSStatus] = useState<WSStatus>("connecting");
  const [rows, setRows] = useState<Record<ProductID, ProductRowState>>(() => {
    const initialRows = {} as Record<ProductID, ProductRowState>;
    for (const productID of PRODUCT_IDS) {
      initialRows[productID] = { status: "loading" };
    }
    return initialRows;
  });

  const [forms, setForms] = useState<Record<ProductID, OverrideFormState>>(() => {
    const initialForms = {} as Record<ProductID, OverrideFormState>;
    for (const productID of PRODUCT_IDS) {
      initialForms[productID] = {
        override_price: "",
        reason: "",
      };
    }
    return initialForms;
  });

  const [actions, setActions] = useState<Record<ProductID, ActionState>>(() => {
    const initialActions = {} as Record<ProductID, ActionState>;
    for (const productID of PRODUCT_IDS) {
      initialActions[productID] = initialActionState;
    }
    return initialActions;
  });

  const [selectedProduct, setSelectedProduct] = useState<ProductID>("PA");
  const selectedProductRef = useRef<ProductID>("PA");
  const reconnectAttemptRef = useRef(0);
  const [historyState, setHistoryState] = useState<HistoryState>({
    loading: true,
    error: "",
    rows: [],
  });
  const productIDSet = useMemo(() => new Set<ProductID>(PRODUCT_IDS), []);

  useEffect(() => {
    const stored = window.sessionStorage.getItem(OPERATOR_KEY_STORAGE_KEY) ?? "";
    setOperatorKey(stored);
  }, []);

  useEffect(() => {
    selectedProductRef.current = selectedProduct;
  }, [selectedProduct]);

  const refreshProduct = useCallback(async (productID: ProductID) => {
    const result = await requestJSON<PriceResponse>(`/api/price/${productID}`);

    setRows((current) => {
      const next = { ...current };
      if (result.ok && result.data) {
        next[productID] = {
          status: "ok",
          payload: result.data,
        };
        return next;
      }

      if (result.status === 404) {
        next[productID] = {
          status: "missing",
          message: result.error || "product not found",
        };
        return next;
      }

      next[productID] = {
        status: "error",
        message: result.error || "failed to load product",
      };
      return next;
    });
  }, []);

  const refreshAllProducts = useCallback(async () => {
    await Promise.all(PRODUCT_IDS.map((id) => refreshProduct(id)));
  }, [refreshProduct]);

  const refreshHistory = useCallback(
    async (productID: ProductID, showLoading: boolean) => {
      const operatorKeyValue = operatorKey.trim();
      if (operatorKeyValue === "") {
        setHistoryState((current) => ({
          loading: false,
          error: "enter operator api key to load history",
          rows: showLoading ? [] : current.rows,
        }));
        return;
      }

      if (showLoading) {
        setHistoryState((current) => ({ ...current, loading: true, error: "" }));
      } else {
        setHistoryState((current) => ({ ...current, error: "" }));
      }
      const result = await requestJSON<HistoryResponse>(`/api/history/${productID}?limit=50`, {
        headers: {
          [OPERATOR_KEY_HEADER]: operatorKeyValue,
        },
      });

      if (!result.ok || !result.data) {
        const message = result.status === 401 ? "invalid operator api key" : result.error || "failed to load history";
        setHistoryState((current) => ({
          loading: false,
          error: message,
          rows: showLoading ? [] : current.rows,
        }));
        return;
      }

      setHistoryState({
        loading: false,
        error: "",
        rows: result.data.history,
      });
    },
    [operatorKey]
  );

  useEffect(() => {
    void refreshAllProducts();
  }, [refreshAllProducts]);

  useEffect(() => {
    void refreshHistory(selectedProduct, true);
  }, [refreshHistory, selectedProduct]);

  useEffect(() => {
    let socket: WebSocket | null = null;
    let reconnectTimer: number | undefined;
    let isCleanup = false;

    const connect = () => {
      if (isCleanup) {
        return;
      }

      setWSStatus(reconnectAttemptRef.current > 0 ? "reconnecting" : "connecting");
      socket = new WebSocket(PRICE_WS_URL);

      socket.onopen = () => {
        reconnectAttemptRef.current = 0;
        setWSStatus("live");
      };

      socket.onmessage = (event) => {
        const update = parsePriceUpdateEvent(event.data);
        if (!update) {
          return;
        }

        const productID = update.product_id as ProductID;
        if (!productIDSet.has(productID)) {
          return;
        }

        void refreshProduct(productID);
        if (selectedProductRef.current === productID) {
          void refreshHistory(productID, false);
        }
      };

      socket.onerror = () => {
        socket?.close();
      };

      socket.onclose = () => {
        if (isCleanup) {
          return;
        }

        setWSStatus("reconnecting");
        const delayIdx = Math.min(reconnectAttemptRef.current, WS_RECONNECT_DELAYS_MS.length - 1);
        const delay = WS_RECONNECT_DELAYS_MS[delayIdx];
        reconnectAttemptRef.current += 1;

        reconnectTimer = window.setTimeout(() => {
          connect();
        }, delay);
      };
    };

    connect();

    return () => {
      isCleanup = true;
      setWSStatus("disconnected");
      if (reconnectTimer !== undefined) {
        window.clearTimeout(reconnectTimer);
      }
      socket?.close();
    };
  }, [productIDSet, refreshHistory, refreshProduct]);

  const chartData = useMemo(
    () => {
      const sortedRows = [...historyState.rows].sort(
        (a, b) => new Date(a.event_time).getTime() - new Date(b.event_time).getTime()
      );

      const selectedRow = rows[selectedProduct];
      const overrideActive = selectedRow.status === "ok" && selectedRow.payload.price_source === "override";

      let visibleRows = sortedRows;
      if (overrideActive) {
        const lastOverrideIndex = sortedRows.reduce((lastIndex, row, index) => {
          if (row.price_source === "override") {
            return index;
          }
          return lastIndex;
        }, -1);
        if (lastOverrideIndex >= 0) {
          visibleRows = sortedRows.slice(0, lastOverrideIndex + 1);
        }
      }

      return visibleRows.map((row) => ({
        time: formatTimestamp(row.event_time),
        price: Number(row.price.toFixed(2)),
        source: row.price_source,
      }));
    },
    [historyState.rows, rows, selectedProduct]
  );

  const hasBasePoint = useMemo(
    () => historyState.rows.some((row) => row.price_source === "base"),
    [historyState.rows]
  );

  const hasOverridePoint = useMemo(
    () => historyState.rows.some((row) => row.price_source === "override"),
    [historyState.rows]
  );

  const renderHistoryDot = (props: {
    cx?: number;
    cy?: number;
    index?: number;
    payload?: { source?: string; time?: string };
  }) => {
    const dotKey = `history-dot-${props.index ?? "na"}-${props.payload?.time ?? "no-time"}-${props.cx ?? 0}-${props.cy ?? 0}`;
    if (props.cx === undefined || props.cy === undefined) {
      return <circle key={`${dotKey}-empty`} cx={0} cy={0} r={0} fill="transparent" stroke="transparent" />;
    }

    const isOverride = props.payload?.source === "override";
    const fill = isOverride ? "#ef4444" : "#0ea5e9";
    return <circle key={dotKey} cx={props.cx} cy={props.cy} r={3.5} fill={fill} stroke={fill} strokeWidth={1} />;
  };

  const updateFormField = (productID: ProductID, field: keyof OverrideFormState, value: string) => {
    setForms((current) => ({
      ...current,
      [productID]: {
        ...current[productID],
        [field]: value,
      },
    }));
  };

  const updateOperatorKey = (value: string) => {
    setOperatorKey(value);
    const trimmedValue = value.trim();
    if (trimmedValue === "") {
      window.sessionStorage.removeItem(OPERATOR_KEY_STORAGE_KEY);
      return;
    }
    window.sessionStorage.setItem(OPERATOR_KEY_STORAGE_KEY, value);
  };

  const setAction = (productID: ProductID, next: ActionState) => {
    setActions((current) => ({
      ...current,
      [productID]: next,
    }));
  };

  const handleUpsertOverride = async (productID: ProductID) => {
    const form = forms[productID];
    const parsedPrice = Number.parseFloat(form.override_price);
    const operatorKeyValue = operatorKey.trim();

    if (!Number.isFinite(parsedPrice) || parsedPrice <= 0) {
      setAction(productID, {
        pending: false,
        kind: "error",
        message: "enter a valid override price",
      });
      return;
    }

    if (operatorKeyValue === "") {
      setAction(productID, {
        pending: false,
        kind: "error",
        message: "enter operator api key first",
      });
      return;
    }

    setAction(productID, {
      pending: true,
      kind: "idle",
      message: "saving override...",
    });

    const payload: OverrideRequest = {
      product_id: productID,
      override_price: parsedPrice,
      reason: form.reason.trim() || undefined,
    };

    const result = await requestJSON<OverrideResponse>("/api/override", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        [OPERATOR_KEY_HEADER]: operatorKeyValue,
      },
      body: JSON.stringify(payload),
    });

    if (!result.ok) {
      const message = result.status === 401 ? "invalid operator api key" : result.error || "failed to save override";
      setAction(productID, {
        pending: false,
        kind: "error",
        message,
      });
      return;
    }

    setAction(productID, {
      pending: false,
      kind: "success",
      message: "override saved",
    });

    await refreshProduct(productID);
    if (selectedProduct === productID) {
      await refreshHistory(productID, false);
    }
  };

  const handleDeleteOverride = async (productID: ProductID) => {
    const operatorKeyValue = operatorKey.trim();
    if (operatorKeyValue === "") {
      setAction(productID, {
        pending: false,
        kind: "error",
        message: "enter operator api key first",
      });
      return;
    }

    setAction(productID, {
      pending: true,
      kind: "idle",
      message: "clearing override...",
    });

    const result = await requestJSON<{ deleted: boolean; product_id: string }>(`/api/override/${productID}`, {
      method: "DELETE",
      headers: {
        [OPERATOR_KEY_HEADER]: operatorKeyValue,
      },
    });

    if (!result.ok) {
      const message = result.status === 401 ? "invalid operator api key" : result.error || "failed to clear override";
      setAction(productID, {
        pending: false,
        kind: "error",
        message,
      });
      return;
    }

    setAction(productID, {
      pending: false,
      kind: "success",
      message: "override cleared",
    });

    await refreshProduct(productID);
    if (selectedProduct === productID) {
      await refreshHistory(productID, false);
    }
  };

  return (
    <main className="p-4 sm:p-6 lg:p-10">
      <div className="mx-auto max-w-7xl animate-rise space-y-6">
        <header className="panel rounded-2xl px-6 py-5">
          <p className="mono text-xs uppercase tracking-[0.18em] text-ink-700">Phase 5 Dashboard</p>
          <div className="mt-2 flex flex-wrap items-end justify-between gap-4">
            <h1 className="text-2xl font-bold text-ink-900 sm:text-3xl">Real-Time Dynamic Pricing Monitor</h1>
            <div className="flex flex-col items-start gap-2 sm:items-end">
              <label className="flex flex-col gap-1 text-xs font-medium uppercase tracking-wide text-ink-700">
                Operator API Key
                <input
                  type="password"
                  className="w-64 rounded-md border border-skyline/35 bg-white px-3 py-2 text-sm font-normal normal-case tracking-normal text-ink-900 outline-none focus:border-skyline"
                  placeholder="Required for override/history"
                  value={operatorKey}
                  onChange={(event) => updateOperatorKey(event.target.value)}
                />
              </label>
              <div className="rounded-full bg-white/80 px-4 py-2 text-sm text-ink-700">
                Live updates: {wsStatus}
              </div>
              <p className="text-xs text-ink-700">
                {operatorKey.trim() === "" ? "Set key to unlock override/history actions." : "Operator key active for this tab."}
              </p>
            </div>
          </div>
        </header>

        <section className="grid gap-6 xl:grid-cols-[1.75fr_1fr]">
          <div className="panel rounded-2xl p-4 sm:p-5">
            <h2 className="mb-4 text-lg font-semibold text-ink-900">Live Product Prices</h2>

            <div className="hidden overflow-x-auto md:block">
              <table className="min-w-full border-collapse text-sm">
                <thead>
                  <tr className="text-left text-xs uppercase tracking-wide text-ink-700">
                    <th className="px-3 py-2">Product</th>
                    <th className="px-3 py-2">Price</th>
                    <th className="px-3 py-2">Demand</th>
                    <th className="px-3 py-2">Source</th>
                    <th className="px-3 py-2">Override Controls</th>
                  </tr>
                </thead>
                <tbody>
                  {PRODUCT_IDS.map((productID) => {
                    const row = rows[productID];
                    const action = actions[productID];

                    return (
                      <tr
                        key={productID}
                        className={`border-t border-skyline/20 transition ${
                          selectedProduct === productID ? "bg-skyline/10" : "hover:bg-white/70"
                        }`}
                        onClick={() => setSelectedProduct(productID)}
                      >
                        <td className="px-3 py-3 font-semibold text-ink-900">{productID}</td>
                        <td className="px-3 py-3 text-ink-900">
                          {row.status === "ok" ? formatCurrency(row.payload.price) : "--"}
                        </td>
                        <td className="px-3 py-3 text-ink-900">{row.status === "ok" ? row.payload.demand : "--"}</td>
                        <td className="px-3 py-3">
                          {row.status === "ok" ? (
                            <span
                              className={`inline-flex rounded-full px-2.5 py-1 text-xs font-medium ${
                                row.payload.price_source === "override"
                                  ? "bg-emberline/25 text-amber-800"
                                  : "bg-mintline/25 text-teal-800"
                              }`}
                            >
                              {row.payload.price_source}
                            </span>
                          ) : (
                            <span className="text-xs text-ink-700">{row.status === "loading" ? "loading..." : row.message}</span>
                          )}
                        </td>
                        <td className="px-3 py-3">
                          <div className="flex flex-wrap items-center gap-2">
                            <input
                              className="w-28 rounded-md border border-skyline/30 bg-white px-2 py-1 text-sm text-ink-900 outline-none focus:border-skyline"
                              type="number"
                              step="0.01"
                              min="0"
                              placeholder="Price"
                              value={forms[productID].override_price}
                              onChange={(event) => updateFormField(productID, "override_price", event.target.value)}
                              onClick={(event) => event.stopPropagation()}
                            />
                            <input
                              className="w-32 rounded-md border border-skyline/30 bg-white px-2 py-1 text-sm text-ink-900 outline-none focus:border-skyline"
                              type="text"
                              placeholder="Reason"
                              value={forms[productID].reason}
                              onChange={(event) => updateFormField(productID, "reason", event.target.value)}
                              onClick={(event) => event.stopPropagation()}
                            />
                            <button
                              type="button"
                              className="rounded-md bg-ink-900 px-2.5 py-1 text-xs font-semibold text-white hover:bg-ink-700 disabled:cursor-not-allowed disabled:opacity-60"
                              disabled={action.pending}
                              onClick={(event) => {
                                event.stopPropagation();
                                void handleUpsertOverride(productID);
                              }}
                            >
                              Set / Update
                            </button>
                            <button
                              type="button"
                              className="rounded-md border border-ink-700 px-2.5 py-1 text-xs font-semibold text-ink-700 hover:bg-ink-700 hover:text-white disabled:cursor-not-allowed disabled:opacity-60"
                              disabled={action.pending}
                              onClick={(event) => {
                                event.stopPropagation();
                                void handleDeleteOverride(productID);
                              }}
                            >
                              Remove Override
                            </button>
                          </div>
                          {action.kind !== "idle" && (
                            <p
                              className={`mt-2 text-xs ${
                                action.kind === "success" ? "text-teal-700" : "text-red-700"
                              }`}
                            >
                              {action.message}
                            </p>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>

            <div className="space-y-3 md:hidden">
              {PRODUCT_IDS.map((productID) => {
                const row = rows[productID];
                const action = actions[productID];

                return (
                  <article
                    key={productID}
                    className={`rounded-xl border border-skyline/20 bg-white/85 p-3 ${
                      selectedProduct === productID ? "ring-2 ring-skyline/40" : ""
                    }`}
                    onClick={() => setSelectedProduct(productID)}
                  >
                    <div className="flex items-center justify-between">
                      <h3 className="text-base font-semibold text-ink-900">{productID}</h3>
                      {row.status === "ok" ? (
                        <span
                          className={`inline-flex rounded-full px-2.5 py-1 text-xs font-medium ${
                            row.payload.price_source === "override"
                              ? "bg-emberline/25 text-amber-800"
                              : "bg-mintline/25 text-teal-800"
                          }`}
                        >
                          {row.payload.price_source}
                        </span>
                      ) : (
                        <span className="text-xs text-ink-700">{row.status === "loading" ? "loading..." : row.message}</span>
                      )}
                    </div>

                    <div className="mt-2 grid grid-cols-2 gap-2 text-sm text-ink-900">
                      <p>
                        <span className="text-ink-700">Price:</span>{" "}
                        {row.status === "ok" ? formatCurrency(row.payload.price) : "--"}
                      </p>
                      <p>
                        <span className="text-ink-700">Demand:</span> {row.status === "ok" ? row.payload.demand : "--"}
                      </p>
                    </div>

                    <div className="mt-3 grid gap-2">
                      <input
                        className="rounded-md border border-skyline/30 bg-white px-2 py-1 text-sm text-ink-900 outline-none focus:border-skyline"
                        type="number"
                        step="0.01"
                        min="0"
                        placeholder="Override price"
                        value={forms[productID].override_price}
                        onChange={(event) => updateFormField(productID, "override_price", event.target.value)}
                        onClick={(event) => event.stopPropagation()}
                      />
                      <input
                        className="rounded-md border border-skyline/30 bg-white px-2 py-1 text-sm text-ink-900 outline-none focus:border-skyline"
                        type="text"
                        placeholder="Reason"
                        value={forms[productID].reason}
                        onChange={(event) => updateFormField(productID, "reason", event.target.value)}
                        onClick={(event) => event.stopPropagation()}
                      />
                      <div className="flex gap-2">
                        <button
                          type="button"
                          className="flex-1 rounded-md bg-ink-900 px-2.5 py-1.5 text-xs font-semibold text-white hover:bg-ink-700 disabled:cursor-not-allowed disabled:opacity-60"
                          disabled={action.pending}
                          onClick={(event) => {
                            event.stopPropagation();
                            void handleUpsertOverride(productID);
                          }}
                        >
                          Set / Update
                        </button>
                        <button
                          type="button"
                          className="flex-1 rounded-md border border-ink-700 px-2.5 py-1.5 text-xs font-semibold text-ink-700 hover:bg-ink-700 hover:text-white disabled:cursor-not-allowed disabled:opacity-60"
                          disabled={action.pending}
                          onClick={(event) => {
                            event.stopPropagation();
                            void handleDeleteOverride(productID);
                          }}
                        >
                          Remove Override
                        </button>
                      </div>
                      {action.kind !== "idle" && (
                        <p className={`text-xs ${action.kind === "success" ? "text-teal-700" : "text-red-700"}`}>
                          {action.message}
                        </p>
                      )}
                    </div>
                  </article>
                );
              })}
            </div>
          </div>

          <aside className="panel rounded-2xl p-4 sm:p-5">
            <h2 className="text-lg font-semibold text-ink-900">Price History</h2>
            <p className="mt-1 text-sm text-ink-700">Selected product: {selectedProduct}</p>

            <div className="mt-4 h-[290px] rounded-xl bg-white/80 p-3">
              {historyState.loading ? (
                <div className="flex h-full items-center justify-center text-sm text-ink-700">Loading history...</div>
              ) : historyState.error ? (
                <div className="flex h-full items-center justify-center text-center text-sm text-red-700">
                  {historyState.error}
                </div>
              ) : chartData.length === 0 ? (
                <div className="flex h-full items-center justify-center text-sm text-ink-700">
                  No history available for this product.
                </div>
              ) : (
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={chartData} margin={{ top: 10, right: 12, bottom: 4, left: 2 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="rgba(14,165,233,0.18)" />
                    <XAxis dataKey="time" tick={{ fill: "#1f4b73", fontSize: 11 }} />
                    <YAxis tick={{ fill: "#1f4b73", fontSize: 11 }} domain={["auto", "auto"]} width={45} />
                    <Tooltip
                      contentStyle={{
                        borderRadius: 10,
                        border: "1px solid rgba(14,165,233,0.25)",
                        backgroundColor: "rgba(255,255,255,0.95)",
                      }}
                    />
                    <Line
                      type="monotone"
                      dataKey="price"
                      stroke="#0ea5e9"
                      strokeWidth={2.5}
                      dot={renderHistoryDot}
                      activeDot={{ r: 5 }}
                    />
                  </LineChart>
                </ResponsiveContainer>
              )}
            </div>

            <p className="mt-3 text-xs text-ink-700">
              {hasBasePoint
                ? hasOverridePoint
                  ? "History includes base price (blue), dynamic prices (blue), and override points (red)."
                  : "History includes the seeded base catalog price point and subsequent dynamic prices."
                : hasOverridePoint
                  ? "History includes dynamic prices (blue) and override points (red)."
                  : "History charts dynamic computed prices from Oracle `price_history`."}
            </p>
          </aside>
        </section>
      </div>
    </main>
  );
}
