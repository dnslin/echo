import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import App from './App'

describe('App bootstrap smoke', () => {
  it('renders visible echo bootstrap content', () => {
    render(<App />)

    expect(screen.getByRole('heading', { name: /echo desktop bootstrap/i })).toBeVisible()
    expect(screen.getByText(/engineering skeleton ready/i)).toBeVisible()
  })
})
