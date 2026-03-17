export type PriceSource = "dynamic" | "override";

export type PriceResponse = {
  product_id: string;
  price: number;
  demand: number;
  price_source: PriceSource;
};

export type ErrorResponse = {
  error: string;
};

export type OverrideRequest = {
  product_id: string;
  override_price: number;
  reason?: string;
};

export type OverrideResponse = {
  product_id: string;
  override_price: number;
  reason: string;
  status: string;
};

export type HistoryEntry = {
  price: number;
  event_time: string;
  price_source: "base" | "dynamic" | "override";
};

export type HistoryResponse = {
  product_id: string;
  history: HistoryEntry[];
};

export type PriceUpdateEvent = {
  type: "price_update";
  product_id: string;
  new_price: number;
  timestamp: number;
};
