// src/core/services/auth.service.ts

import { BrowserWindow, safeStorage, shell } from "electron";
import { GetSessionURL, OAuthCallbackUrl, OAuthSigninURL, SignOutUrl } from "@core/const";
import { fetcher } from "@core/lib/fetcher";
import { db } from "@core/db";

interface session {
  id: string;
  userId: string;
  userAgent: string;
  ipAddress: string;
  updatedAt: Date;
  createdAt: Date;
  token: string;
  expiresAt: Date;
}

interface user {
  id: string;
  name: string
  email: string
  emailVerified: boolean;
  image: string | null;
  createdAt: Date;
  updatedAt: Date;
  username: string;
  displayUsername: string | null;
  isStaff: boolean;
}

export interface GetSessionRes {
  session: session;
  user: user
}

class AuthService {
  session = {
    get: async () => {
      const encStr = await db.get("LocalStorage", "sess");
      if (!encStr) return null;

      return safeStorage.decryptString(Buffer.from(encStr, 'base64'));
    },

    set: async (token: string) => {
      const encrypted = safeStorage.encryptString(token).toString("base64");
      db.update("LocalStorage", "sess", encrypted);
    },

    del: async () => {
      db.update("LocalStorage", "sess", null);
    }
  }

  async CheckSessionState() {
    const mainWindow = BrowserWindow.getAllWindows()[0];
    if (!mainWindow) {
      throw new Error("메인 창을 가져오는데 실패함");
    }

    const sessKey = await this.session.get();
    if (!sessKey) {
      mainWindow.webContents.send('auth.CheckSessionState', false);
      return false;
    }

    const res = await fetcher<GetSessionRes>(GetSessionURL, { sessionToken: sessKey });

    if (!res.data?.session) {
      if (sessKey) {
        await db.update('LocalStorage', 'sess', null);
      }

      mainWindow.webContents.send('auth.CheckSessionState', false);
      mainWindow.webContents.send('auth-state-changed', false)
      return false;
    }

    mainWindow.webContents.send('auth.CheckSessionState', true);
    return true
  }

  async StartOAuth2Login() {
    const session = await this.CheckSessionState();
    if (session) return;

    const resp = await fetcher<{ url: string, redirect: boolean }>(OAuthSigninURL, {
      method: "POST",
      body: {
        providerId: "nahida",
        callbackURL: ""
      }
    });

    shell.openExternal(resp.data.url);
  }

  async handleDeepLink(url: string) {
    if (url.startsWith('nahida-desktop://auth') || url.startsWith('nahida://auth')) {
      try {
        await this.handleOAuth2Callback(url);
      } catch (error) {
        console.error('딥링크 처리 중 오류:', error);
      }
    }
  }

  async handleOAuth2Callback(url: string) {
    const getHeader = (headers: any, name: string): string | null => {
      const lowerName = name.toLowerCase();
      for (const key in headers) {
        if (key.toLowerCase() === lowerName) {
          return headers[key];
        }
      }
      return null;
    };

    const mainWindow = BrowserWindow.getAllWindows()[0];

    try {
      const urlObj = new URL(url);
      const code = urlObj.searchParams.get('code');
      const state = urlObj.searchParams.get('state');

      if (!code || !state) {
        throw new Error('인증 코드나 상태를 찾을 수 없습니다.');
      }

      const requestUrl = `${OAuthCallbackUrl}/nahida?code=${encodeURIComponent(code)}&state=${encodeURIComponent(state)}`;

      const response = await fetcher<{ token: string }>(requestUrl, {
        maxRedirects: 0
      });

      const authToken = getHeader(response.headers, 'set-auth-token');

      if (authToken) {
        await this.session.set(authToken);
        mainWindow.webContents.send('auth-state-changed', true)
        return true;
      } else {
        if (response.data && typeof response.data === 'object' && response.data.token) {
          await this.session.set(response.data.token);
          mainWindow.webContents.send('auth-state-changed', true)
          return true;
        }
      }
    } catch (error: any) {
      if (error.response && error.response.status === 302) {
        const headers = error.response.headers;

        const possibleHeaderNames = [
          'set-auth-token',
          'Set-Auth-Token',
          'auth-token',
          'Auth-Token',
          'authtoken',
          'AuthToken',
          'x-auth-token',
          'X-Auth-Token'
        ];

        let authToken: string | null = null;
        for (const headerName of possibleHeaderNames) {
          authToken = getHeader(headers, headerName);
          if (authToken) {
            break;
          }
        }

        if (authToken) {
          await this.session.set(authToken);
          return true;
        } else {
          const location = getHeader(headers, 'location');
          if (location) {
            try {
              const locationUrl = new URL(location);
              const tokenParam = locationUrl.searchParams.get('token');
              if (tokenParam) {
                await this.session.set(tokenParam);
                return true;
              }
            } catch (parseError) {
              console.error('Location URL 파싱 오류:', parseError);
            }
          }

          if (error.response.data && typeof error.response.data === 'object' && error.response.data.token) {
            await this.session.set(error.response.data.token);
            return true;
          }

          mainWindow.webContents.send('auth-state-changed', false)
          return false;
        }
      }

      throw error;
    }

    return false;
  } catch(err: any) {
    console.error('OAuth2 콜백 처리 중 오류:', err.message);
    return false;
  }

  async Logout() {
    const mainWindow = BrowserWindow.getAllWindows()[0];

    const resp = await fetcher<{ success: boolean; }>(SignOutUrl, {
      method: 'POST'
    })

    if (resp.ok) {
      await this.session.del();
      mainWindow.webContents.send('auth-state-changed', false)
    }
  }
}


const auth = new AuthService();

export { auth };