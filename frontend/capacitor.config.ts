import type { CapacitorConfig } from '@capacitor/cli';

const config: CapacitorConfig = {
  appId: 'com.healthlogin.app',
  appName: 'healthlogin',
  webDir: 'dist',
  androidScheme: 'https',
  server: {
    cleartext: true,
    allowNavigation: ['94.103.9.172:*', '*'],
  },
  plugins: {
    CapacitorHttp: {
      enabled: true,
    },
  },
};

export default config;
