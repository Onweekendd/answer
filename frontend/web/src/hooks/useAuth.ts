import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useSyncExternalStore } from 'react'
import { needsEmailVerification } from '@/core/api/endpoints'
import { api, tokenStore } from '@/lib/session'

export function useIsLoggedIn() {
  return useSyncExternalStore(tokenStore.subscribe, tokenStore.isLoggedIn)
}

export function useLogin() {
  return useMutation({
    mutationFn: ({ email, password }: { email: string; password: string }) => api.login(email, password),
    onSuccess: (resp) => tokenStore.set(resp.access_token),
  })
}

/**
 * Registering logs the user in straight away unless the site wants the mail verified first;
 * then `data.needsVerification` is true and no token is stored.
 */
export function useRegister() {
  return useMutation({
    mutationFn: async ({ name, email, password }: { name: string; email: string; password: string }) => {
      const resp = await api.register(name, email, password)
      return { resp, needsVerification: needsEmailVerification(resp) }
    },
    onSuccess: ({ resp, needsVerification }) => {
      if (!needsVerification) tokenStore.set(resp.access_token)
    },
  })
}

/** Drops the token and everything cached for the old user. */
export function useLogout() {
  const qc = useQueryClient()
  return () => {
    tokenStore.clear()
    qc.clear()
  }
}
