import { Navigate, Outlet, createBrowserRouter } from 'react-router-dom'
import { AppShell } from '@/components/AppShell'
import { useIsLoggedIn } from '@/hooks/useAuth'
import Chat from '@/pages/Chat'
import Community from '@/pages/Community'
import Login from '@/pages/Login'

/** Everything except /login needs a token; a cleared token (logout, 401) lands here too. */
function RequireAuth() {
  return useIsLoggedIn() ? <Outlet /> : <Navigate to="/login" replace />
}

export const router = createBrowserRouter([
  { path: '/login', element: <Login /> },
  {
    element: <RequireAuth />,
    children: [
      {
        element: <AppShell />,
        children: [
          { index: true, element: <Chat /> },
          { path: 'community', element: <Community /> },
        ],
      },
    ],
  },
  { path: '*', element: <Navigate to="/" replace /> },
])
