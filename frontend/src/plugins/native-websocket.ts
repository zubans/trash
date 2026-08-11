import { registerPlugin, PluginListenerHandle } from '@capacitor/core'

export interface NativeWebSocketPlugin {
  connect(options: { url: string }): Promise<void>
  send(options: { message: string }): Promise<void>
  disconnect(): Promise<void>
  addListener(eventName: 'onOpen', listenerFunc: (data: any) => void): Promise<PluginListenerHandle>
  addListener(eventName: 'onMessage', listenerFunc: (data: { data: string }) => void): Promise<PluginListenerHandle>
  addListener(eventName: 'onClose', listenerFunc: (data: any) => void): Promise<PluginListenerHandle>
  addListener(eventName: 'onError', listenerFunc: (data: { error: string }) => void): Promise<PluginListenerHandle>
}

export const NativeWebSocket = registerPlugin<NativeWebSocketPlugin>('NativeWebSocket', {
  web: async () => {
    return {
      connect: async () => {},
      send: async () => {},
      disconnect: async () => {},
      addListener: async () => ({ remove: async () => {} }),
    } as NativeWebSocketPlugin
  },
})
