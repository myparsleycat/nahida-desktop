// src/core/services/toast.service.ts

import { BrowserWindow } from 'electron';
import type { ExternalToast } from "svelte-sonner";

export interface ToastMessage {
  message: string;
  type?: 'default' | 'success' | 'error' | 'warning' | 'info';
  duration?: number;
}

class ToastServiceClass {
  private mainWindow?: BrowserWindow;

  setMainWindow(window: BrowserWindow) {
    this.mainWindow = window;
  }

  show(message: string, type: ToastMessage['type'] = 'default', data?: ExternalToast) {
    if (this.mainWindow && this.mainWindow.webContents) {
      this.mainWindow.webContents.send('toast-show', {
        message,
        type,
        data
      });
    }
  }

  success(message: string, data?: ExternalToast) {
    this.show(message, 'success', data);
  }

  error(message: string, data?: ExternalToast) {
    this.show(message, 'error', data);
  }

  warning(message: string, data?: ExternalToast) {
    this.show(message, 'warning', data);
  }

  info(message: string, data?: ExternalToast) {
    this.show(message, 'info', data);
  }
}

export const ToastService = new ToastServiceClass();