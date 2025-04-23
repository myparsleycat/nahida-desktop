import { writable, get, derived } from 'svelte/store';

class AuthHelper {
  checkingSession = writable(true);
  loggedIn = writable(false);

  async CheckSessionState() {
    return await window.api.auth.checkSessionState();
  }

  async StartOAuth2Login() {
    await window.api.auth.startOAuth2Login();
  }

  async Logout() {
    await window.api.auth.logout();
  }
}

const Auth = new AuthHelper;
export { Auth };