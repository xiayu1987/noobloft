// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT


import { t, locale } from '../i18n/index'
export async function request(path: string, method = 'GET', body?: unknown, revision?: string, signal?: AbortSignal): Promise<Response> {
  const response = await fetch('/manage/' + path, { method, signal, credentials: 'same-origin', headers: { 'Accept-Language': locale.value, ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}), ...(revision ? { 'If-Match': revision } : {}) }, body: body === undefined ? undefined : JSON.stringify(body) })
  if (!response.ok) {
    const result = await response.json().catch(() => ({}))
    throw new Error(result.error?.message || `HTTP ${response.status}`)
  }
  return response
}
export async function api(path: string, method = 'GET', body?: unknown, revision?: string) {
  return (await request(path, method, body, revision)).json()
}
export async function streamChat(body: unknown, signal: AbortSignal, delta: (text: string, reasoning: string) => void) {
  const response = await request('chat', 'POST', body, undefined, signal)
  if (!response.body) throw new Error(t('stream.unsupported'))
  const reader = response.body.getReader(), decoder = new TextDecoder()
  let buffer = '', done = false
  const consume = (line: string) => {
    if (!line.startsWith('data:')) return
    const data = line.slice(5).trim()
    if (data === '[DONE]') { done = true; return }
    if (!data) return
    const value = JSON.parse(data)
    if (value.error) throw new Error(value.error.message)
    delta(value.choices?.[0]?.delta?.content || '', value.choices?.[0]?.delta?.reasoning_content || '')
  }
  try {
    while (!done) {
      const chunk = await reader.read()
      buffer += decoder.decode(chunk.value, { stream: !chunk.done })
      let i: number
      while ((i = buffer.indexOf('\n')) >= 0) { consume(buffer.slice(0, i).replace(/\r$/, '')); buffer = buffer.slice(i + 1) }
      if (chunk.done) { if (buffer) consume(buffer); if (!done) throw new Error(t('stream.incomplete')); break }
    }
  } finally { await reader.cancel().catch(() => {}); reader.releaseLock() }
}
