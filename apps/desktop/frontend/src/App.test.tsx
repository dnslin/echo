import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import App from './App'

describe('App bootstrap smoke', () => {
  it('renders visible echo bootstrap content', () => {
    render(<App />)

    expect(screen.getByRole('heading', { name: /echo 桌面端已就绪/i })).toBeVisible()
    expect(screen.getByText(/工程骨架已准备好/i)).toBeVisible()
  })
})
