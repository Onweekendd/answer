import { useState, type FormEvent } from 'react'
import { Navigate } from 'react-router-dom'
import { ThemeToggle } from '@/components/ThemeToggle'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { describeError, fieldErrors } from '@/core/api/errors'
import { useIsLoggedIn, useLogin, useRegister } from '@/hooks/useAuth'

function Field({ id, label, error, ...props }: { id: string; label: string; error?: string } & React.ComponentProps<typeof Input>) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Input id={id} aria-invalid={Boolean(error)} {...props} />
      {error && <p className="text-xs text-destructive">{error}</p>}
    </div>
  )
}

function LoginForm() {
  const login = useLogin()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const fields = fieldErrors(login.error)

  const submit = (e: FormEvent) => {
    e.preventDefault()
    login.mutate({ email, password })
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <Field id="login-email" label="邮箱" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} error={fields.e_mail} />
      <Field id="login-pass" label="密码" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} error={fields.pass} />
      {login.error && Object.keys(fields).length === 0 && <p className="text-sm text-destructive">{describeError(login.error)}</p>}
      <Button type="submit" size="lg" className="w-full" disabled={login.isPending}>
        {login.isPending ? '登录中…' : '登录'}
      </Button>
    </form>
  )
}

function RegisterForm() {
  const register = useRegister()
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const fields = fieldErrors(register.error)

  const submit = (e: FormEvent) => {
    e.preventDefault()
    register.mutate({ name, email, password })
  }

  if (register.data?.needsVerification) {
    return <p className="py-6 text-center text-sm">注册成功，请到邮箱点击验证链接后再登录。</p>
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <Field id="reg-name" label="用户名" autoComplete="username" required minLength={2} maxLength={30} value={name} onChange={(e) => setName(e.target.value)} error={fields.name} />
      <Field id="reg-email" label="邮箱" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} error={fields.e_mail} />
      <Field id="reg-pass" label="密码（8–32 位）" type="password" autoComplete="new-password" required minLength={8} maxLength={32} value={password} onChange={(e) => setPassword(e.target.value)} error={fields.pass} />
      {register.error && Object.keys(fields).length === 0 && <p className="text-sm text-destructive">{describeError(register.error)}</p>}
      <Button type="submit" size="lg" className="w-full" disabled={register.isPending}>
        {register.isPending ? '注册中…' : '注册'}
      </Button>
    </form>
  )
}

export default function Login() {
  if (useIsLoggedIn()) return <Navigate to="/" replace />

  return (
    <div className="flex min-h-dvh flex-col items-center justify-center bg-background px-4 py-8">
      <div className="absolute right-3 top-3">
        <ThemeToggle />
      </div>
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle className="text-2xl font-bold tracking-tight">买不买</CardTitle>
          <CardDescription>拿不准的东西，问问 AI 和大家</CardDescription>
        </CardHeader>
        <CardContent>
          <Tabs defaultValue="login">
            <TabsList className="mb-4 grid w-full grid-cols-2">
              <TabsTrigger value="login">登录</TabsTrigger>
              <TabsTrigger value="register">注册</TabsTrigger>
            </TabsList>
            <TabsContent value="login">
              <LoginForm />
            </TabsContent>
            <TabsContent value="register">
              <RegisterForm />
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>
    </div>
  )
}
