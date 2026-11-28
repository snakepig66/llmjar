import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ToastProvider } from '@/ui'

import { MetricsContent } from './metrics'

const payload = {
  window_hours: 24,
  total: {
    requests: 300,
    prompt_tokens: 2_000_000,
    completion_tokens: 100_000,
    total_tokens: 2_100_000,
  },
  channels: [
    {
      channel_id: 52,
      channel_name: 'open design',
      requests: 200,
      prompt_tokens: 1_500_000,
      completion_tokens: 80_000,
      total_tokens: 1_580_000,
      token_share: 0.7524,
      request_share: 0.6667,
    },
    {
      channel_id: 45,
      channel_name: '商汤小浣熊',
      requests: 100,
      prompt_tokens: 500_000,
      completion_tokens: 20_000,
      total_tokens: 520_000,
      token_share: 0.2476,
      request_share: 0.3333,
    },
  ],
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'content-type': 'application/json' },
  })
}

let requestedURLs: string[] = []
let body: unknown = payload

const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
  const url = String(input)
  requestedURLs.push(url)
  if (url.startsWith('/api/option/channel_token_stats')) {
    return jsonResponse({ success: true, data: body })
  }
  return jsonResponse({ success: false, message: 'not found' })
})

beforeEach(() => {
  requestedURLs = []
  body = payload
  fetchMock.mockClear()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 0 } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <MetricsContent />
      </ToastProvider>
    </QueryClientProvider>,
  )
}

describe('admin metrics page', () => {
  it('lists channel token usage with totals and shares', async () => {
    renderPage()

    expect(await screen.findByText('open design')).toBeTruthy()
    expect(screen.getByText('商汤小浣熊')).toBeTruthy()
    expect(screen.getByText('合计')).toBeTruthy()
    expect(screen.getByText('1.6M')).toBeTruthy()
    expect(screen.getByText('75.2%')).toBeTruthy()
    expect(screen.getByText('24.8%')).toBeTruthy()
    expect(requestedURLs[0]).toContain('/api/option/channel_token_stats')
  })

  it('switches time range and refetches with new hours', async () => {
    renderPage()
    await screen.findByText('open design')

    fireEvent.click(screen.getByRole('tab', { name: '本月' }))
    await waitFor(() =>
      expect(
        requestedURLs.some((url) => url.includes('period=month')),
      ).toBe(true),
    )
  })

  it('shows the empty state when the range has no consumption', async () => {
    body = { window_hours: 24, total: { requests: 0, prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 }, channels: [] }
    renderPage()

    expect(await screen.findByText('当前范围内没有消费记录')).toBeTruthy()
  })
})
