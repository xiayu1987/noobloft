// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
export default defineConfig({ plugins: [vue()], build: { outDir: '../internal/controlplane/webui/dist', emptyOutDir: true, rollupOptions: { output: { manualChunks: { 'element-plus': ['element-plus'], vue: ['vue'] } } } }, server: { proxy: { '/manage': {target: 'http://127.0.0.1:8760', changeOrigin: false}, '/healthz': 'http://127.0.0.1:8760' } } })
