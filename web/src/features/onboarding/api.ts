import type { OnboardingAction, OnboardingClient, OnboardingItemKey, OnboardingView } from './types'

export async function fetchOnboarding(client: OnboardingClient) {
  return await client.request('/onboarding') as OnboardingView
}

export async function actOnboarding(client: OnboardingClient, action: OnboardingAction, item?: OnboardingItemKey) {
  return await client.request('/onboarding', { method: 'POST', body: JSON.stringify(item ? { action, item } : { action }) }) as OnboardingView
}

export async function recordTour(client: OnboardingClient, key: string, status: 'started' | 'completed') {
  const result = await client.request('/onboarding/tours', { method: 'POST', body: JSON.stringify({ key, status }) }) as { tours: OnboardingView['tours'] }
  return result.tours
}
