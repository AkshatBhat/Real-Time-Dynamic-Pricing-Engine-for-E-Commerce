export const PRODUCT_IDS = ["PA", "PB", "PC", "PD", "PE", "PF", "PG", "PH", "PI", "PJ"] as const;

export type ProductID = (typeof PRODUCT_IDS)[number];
