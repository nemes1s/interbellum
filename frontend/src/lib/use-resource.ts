"use client";

import { useCallback, useEffect, useRef, useState } from "react";

/**
 * The console's entire data layer: fetch on mount, expose `{data, error,
 * loading, reload, set}`.
 *
 * There is no cache, no query client and no store. Every screen reads one or
 * two resources and the server is the only state that matters — an
 * investigation's truth lives in PostgreSQL, and re-reading it is a request.
 * `set` exists because `POST /decisions` returns the new state, so a successful
 * decision does not need a follow-up GET.
 */
export interface Resource<T> {
  data: T | null;
  error: unknown;
  loading: boolean;
  reload: () => void;
  set: (value: T) => void;
}

export function useResource<T>(
  load: (signal: AbortSignal) => Promise<T>,
  deps: React.DependencyList,
  options: { enabled?: boolean } = {},
): Resource<T> {
  const enabled = options.enabled ?? true;
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(enabled);
  const [nonce, setNonce] = useState(0);

  // Keeps the effect's dependency list stable while still calling the latest
  // closure; the caller's `deps` are the real trigger.
  const loadRef = useRef(load);
  loadRef.current = load;

  useEffect(() => {
    if (!enabled) {
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    setLoading(true);
    setError(null);

    loadRef
      .current(controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) {
          setData(value);
          setLoading(false);
        }
      })
      .catch((err) => {
        if (!controller.signal.aborted) {
          setError(err);
          setLoading(false);
        }
      });

    return () => controller.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, nonce, enabled]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);
  const set = useCallback((value: T) => {
    setData(value);
    setError(null);
  }, []);

  return { data, error, loading, reload, set };
}
