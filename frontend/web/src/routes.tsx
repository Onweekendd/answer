import { createBrowserRouter, Navigate } from "react-router-dom";

import { AppShell } from "@/components/AppShell";
import { RequireAuth } from "@/components/RequireAuth";
import Chat from "@/pages/Chat";
import Community from "@/pages/Community";
import Login from "@/pages/Login";

// 线上在 /app/ 下（见 vite.config.ts 的 base），本地开发在根路径
const basename = import.meta.env.BASE_URL.replace(/\/$/, "") || "/";

export const router = createBrowserRouter(
  [
    { path: "/login", element: <Login /> },
    {
      element: <RequireAuth />,
      children: [
        {
          element: <AppShell />,
          children: [
            { index: true, element: <Chat /> },
            { path: "community", element: <Community /> }
          ]
        }
      ]
    },
    { path: "*", element: <Navigate to="/" replace /> }
  ],
  { basename }
);
