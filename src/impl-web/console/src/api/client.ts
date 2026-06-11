/** API error with status code and body */
export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly statusText: string,
    public readonly body: unknown,
  ) {
    super(`API ${status}: ${statusText}`);
    this.name = 'ApiError';
  }
}

/** Base fetch wrapper — handles JSON parsing and error mapping */
async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...init?.headers,
    },
  });

  if (!res.ok) {
    let body: unknown;
    try {
      body = await res.json();
    } catch {
      body = await res.text().catch(() => null);
    }
    throw new ApiError(res.status, res.statusText, body);
  }

  // 204 No Content
  if (res.status === 204) return undefined as T;

  return res.json() as Promise<T>;
}

/** Typed API helper */
export const api = {
  get: <T>(url: string, signal?: AbortSignal) =>
    request<T>(url, { method: 'GET', signal }),

  post: <T>(url: string, body?: unknown, signal?: AbortSignal) =>
    request<T>(url, {
      method: 'POST',
      body: body != null ? JSON.stringify(body) : undefined,
      signal,
    }),

  put: <T>(url: string, body: unknown, signal?: AbortSignal) =>
    request<T>(url, {
      method: 'PUT',
      body: JSON.stringify(body),
      signal,
    }),

  delete: <T = void>(url: string, signal?: AbortSignal) =>
    request<T>(url, { method: 'DELETE', signal }),
};