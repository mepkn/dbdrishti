import * as service from "../../bindings/github.com/mepkn/drishti/internal/db/service";
import type * as m from "../../bindings/github.com/mepkn/drishti/internal/db/models";

// The generated bindings type every Go slice as `T[] | null`. The service
// always returns empty slices instead of nil, so Fix drops those nulls
// while keeping real ones (NULL values, optional objects).
type Fix<T> = T extends string | number | boolean | null | undefined
  ? T
  : T extends (infer E)[]
    ? FixElem<E>[]
    : { [K in keyof T]: FixField<T[K]> };
type FixElem<E> = Extract<E, unknown[]> extends never ? Fix<E> : Fix<Exclude<E, null>>;
type FixField<V> = Extract<V, unknown[]> extends never ? Fix<V> : Fix<Exclude<V, null>>;

type Service = typeof service;
export const api = service as unknown as {
  [K in keyof Service]: Service[K] extends (...args: infer A) => PromiseLike<infer R> ? (...args: A) => Promise<Fix<R>> : never;
};

export type Analysis = Fix<m.Analysis>;
export type Column = Fix<m.Column>;
export type ExecuteResult = Fix<m.ExecuteResult>;
export type ForeignKey = Fix<m.ForeignKey>;
export type JoinTable = Fix<m.JoinTable>;
export type JoinView = Fix<m.JoinView>;
export type Preview = Fix<m.Preview>;
export type Result = Fix<m.Result>;
export type SQLError = Fix<m.SQLError>;
export type Status = Fix<m.Status>;
export type Step = Fix<m.Step>;
export type Table = Fix<m.Table>;
export type Warning = Fix<m.Warning>;

// Go errors reach the frontend as rejected promises whose message is the text.
export function errorText(e: unknown): string {
  if (e instanceof Error) return e.message;
  if (typeof e === "object" && e && "message" in e) return String((e as { message: unknown }).message);
  return String(e);
}
