import type { CapacitorConfig } from '@capacitor/cli';

const config: CapacitorConfig = {
  appId: 'com.healthlogin.app',
  appName: 'healthlogin',
  webDir: 'dist',
  androidScheme: 'http',
  server: {
    cleartext: true,
    allowNavigation: ['94.103.9.172:*'],
  },
  plugins: {
    CapacitorHttp: {
      enabled: false,
    },
  },
};

export default config;
