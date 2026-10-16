import { Navigate, Outlet } from "react-router-dom";

import { useIsLoggedIn } from "@/hooks/useAuth";

/** Everything except /login needs a token; a cleared token (logout, 401) lands here too. */
export function RequireAuth() {
  return useIsLoggedIn() ? <Outlet /> : <Navigate to="/login" replace />;
}
