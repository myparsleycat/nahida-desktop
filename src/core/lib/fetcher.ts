import axios, { AxiosRequestConfig, AxiosResponse } from 'axios';
import { auth } from "../services/auth.service";

interface FetcherOptions {
  method?: "GET" | "POST" | "PUT" | "DELETE" | "PATCH";
  headers?: Record<string, string>;
  body?: any;
  sessionToken?: string;
  maxRedirects?: number;
}

interface FetcherResponse<T = any> {
  data: T;
  status: number;
  headers: Record<string, string>;
  ok: boolean;
}

async function fetcher<T = any>(url: string, options?: FetcherOptions): Promise<FetcherResponse<T>> {
  const { method = "GET", headers = {}, body, sessionToken, maxRedirects } = options || {};

  const defaultHeaders: Record<string, string> = {
    'User-Agent': 'Nahida Desktop/0.0.1',
    'Content-Type': 'application/json',
  };

  let token: string | null = sessionToken || null;
  if (!token) {
    token = await auth.session.get();
  }

  if (token) {
    defaultHeaders['Cookie'] = `__Secure-nahida.session_token=${token}`;
  }

  const mergedHeaders = { ...defaultHeaders, ...headers };

  const config: AxiosRequestConfig = {
    method,
    headers: mergedHeaders,
    data: body,
    withCredentials: true,
    validateStatus: (status) => {
      return status >= 200 && status < 400;
    },
  };

  if (maxRedirects !== undefined) {
    config.maxRedirects = maxRedirects;
  }

  try {
    const response: AxiosResponse<T> = await axios(url, config);

    const setCookieHeader = response.headers['set-cookie'];
    if (setCookieHeader) {
      const cookieArray = Array.isArray(setCookieHeader) ? setCookieHeader : [setCookieHeader];

      for (const cookie of cookieArray) {
        if (cookie.includes('__Secure-nahida.session_token=')) {
          const tokenMatch = cookie.match(/__Secure-nahida\.session_token=([^;]+)/);
          if (tokenMatch && tokenMatch[1]) {
            await auth.session.set(tokenMatch[1]);
            break;
          }
        }
      }
    }

    return {
      data: response.data,
      status: response.status,
      headers: response.headers as Record<string, string>,
      ok: response.status >= 200 && response.status < 300
    };
  } catch (error) {
    if (axios.isAxiosError(error) && error.response) {
      throw new Error(`HTTP error! Status: ${error.response.status}`);
    }
    throw error;
  }
}

export { fetcher };