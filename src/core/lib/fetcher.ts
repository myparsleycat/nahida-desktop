import axios, { AxiosInstance, AxiosRequestConfig, AxiosResponse } from 'axios';
import { AuthService } from "../services";
import packageJson from "../../../package.json";
import { is } from '@electron-toolkit/utils';

interface CustomResponse<T = any> {
  data: T;
  status: number;
  headers: Record<string, string>;
  ok: boolean;
}

export function createApiClient(baseConfig: AxiosRequestConfig = {}) {
  const instance: AxiosInstance = axios.create({
    ...baseConfig,
    withCredentials: true,
    validateStatus: (status) => status >= 200 && status < 400,
  });

  instance.interceptors.request.use(async (config) => {
    config.headers = config.headers || {};
    config.headers['User-Agent'] = `Nahida Desktop/${packageJson.version}`;
    config.headers['Content-Type'] = 'application/json';

    if (!config.headers['Cookie'] || !config.headers['Cookie'].includes('__Secure-nahida.session_token')) {
      const token = await AuthService.session.get();
      if (token) {
        config.headers['Cookie'] = `__Secure-nahida.session_token=${token}`;
      }
    }

    return config;
  });

  instance.interceptors.response.use(async (response) => {
    const setCookieHeader = response.headers['set-cookie'];
    if (setCookieHeader) {
      const cookieArray = Array.isArray(setCookieHeader) ? setCookieHeader : [setCookieHeader];

      for (const cookie of cookieArray) {
        if (cookie.includes('__Secure-nahida.session_token=')) {
          const tokenMatch = cookie.match(/__Secure-nahida\.session_token=([^;]+)/);
          if (tokenMatch && tokenMatch[1]) {
            await AuthService.session.set(tokenMatch[1]);
            break;
          }
        }
      }
    }
    return response;
  });

  async function request<T = any>(
    url: string,
    config: AxiosRequestConfig = {}
  ): Promise<CustomResponse<T>> {
    try {
      const response: AxiosResponse<T> = await instance(url, config);

      return {
        data: response.data,
        status: response.status,
        headers: response.headers as Record<string, string>,
        ok: response.status >= 200 && response.status < 300,
      };
    } catch (error) {
      if (axios.isAxiosError(error) && error.response) {
        return {
          data: error.response.data as T,
          status: error.response.status,
          headers: error.response.headers as Record<string, string>,
          ok: false,
        };
      }

      throw error;
    }
  }

  const get = <T = any>(url: string, config?: AxiosRequestConfig) =>
    request<T>(url, { ...config, method: 'GET' });

  const post = <T = any>(url: string, data?: any, config?: AxiosRequestConfig) =>
    request<T>(url, { ...config, method: 'POST', data });

  const put = <T = any>(url: string, data?: any, config?: AxiosRequestConfig) =>
    request<T>(url, { ...config, method: 'PUT', data });

  const del = <T = any>(url: string, config?: AxiosRequestConfig) =>
    request<T>(url, { ...config, method: 'DELETE' });

  const patch = <T = any>(url: string, data?: any, config?: AxiosRequestConfig) =>
    request<T>(url, { ...config, method: 'PATCH', data });

  return {
    instance,
    request,
    get,
    post,
    put,
    delete: del,
    patch,
  };
}

export const apiClient = createApiClient();
export const api = apiClient.instance;

export async function fetcher<T = any>(url: string, options?: {
  method?: "GET" | "POST" | "PUT" | "DELETE" | "PATCH";
  headers?: Record<string, string>;
  body?: any;
  sessionToken?: string;
  maxRedirects?: number;
}): Promise<CustomResponse<T>> {
  const { method = "GET", headers = {}, body, sessionToken, maxRedirects } = options || {};

  if (is.dev) {
    console.log('fetcher handled', url);
  }

  const config: AxiosRequestConfig = {
    method,
    headers,
    data: body,
  };

  if (sessionToken) {
    config.headers = {
      ...config.headers,
      Cookie: `__Secure-nahida.session_token=${sessionToken}`
    };
  }

  if (maxRedirects !== undefined) {
    config.maxRedirects = maxRedirects;
  }

  return apiClient.request<T>(url, config);
}