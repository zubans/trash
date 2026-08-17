import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const certsDir = path.resolve(__dirname, '..', 'certs')

function readCert(file: string): Buffer | undefined {
  try {
    return fs.readFileSync(path.join(certsDir, file))
  } catch {
    return undefined
  }
}

const key = readCert('localhost-key.pem')
const cert = readCert('localhost-cert.pem')

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [vue()],
  base: '/',
  server: {
    port: 8443,
    https: key && cert ? { key, cert } : undefined,
  },
  test: {
    environment: 'jsdom',
    globals: true,
  },
})
