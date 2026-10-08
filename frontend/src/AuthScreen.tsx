import { useEffect, useState } from 'react'
import {
  authConfig,
  checkInvite,
  forgotPassword,
  login,
  register,
  resetPassword,
  type AuthConfig,
  type User,
} from './api'
import { authErrorText } from './authErrors'
import { urlTokens } from './urlTokens'

type Mode = 'login' | 'register' | 'forgot' | 'reset'

// The logged-out screen: login, sign-up (open/approval/invite modes, ADR
// 0005), forgot password, and "set a new password" from a reset link.
export default function AuthScreen({
  onAuthenticated,
  initialNotice,
}: {
  onAuthenticated: (u: User) => void
  initialNotice?: string | null
}) {
  const [cfg, setCfg] = useState<AuthConfig | null>(null)
  const [mode, setMode] = useState<Mode>(urlTokens.reset ? 'reset' : 'login')
  const [resetToken, setResetToken] = useState<string | null>(urlTokens.reset)
  const [inviteToken, setInviteToken] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(initialNotice ?? null)
  const [busy, setBusy] = useState(false)

  const [loginName, setLoginName] = useState('')
  const [email, setEmail] = useState('')
  const [username, setUsername] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [password, setPassword] = useState('')
  const [password2, setPassword2] = useState('')

  useEffect(() => {
    authConfig().then(setCfg).catch(() => setCfg({ registration: 'closed', email_enabled: false }))
    const invite = urlTokens.invite
    if (!urlTokens.reset && invite) {
      checkInvite(invite)
        .then((r) => {
          setInviteToken(invite)
          if (r.email) setEmail(r.email)
          setMode('register')
        })
        .catch(() => setError('This invite is invalid or has expired.'))
    }
  }, [])

  function switchMode(m: Mode) {
    setMode(m)
    setError(null)
    setNotice(null)
    setPassword('')
    setPassword2('')
  }

  async function run(fn: () => Promise<void>) {
    setError(null)
    setBusy(true)
    try {
      await fn()
    } catch (e) {
      setError(authErrorText(e))
    } finally {
      setBusy(false)
    }
  }

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    if ((mode === 'register' || mode === 'reset') && password !== password2) {
      setError('The two passwords do not match.')
      return
    }
    if (mode === 'login') {
      run(async () => onAuthenticated(await login(loginName.trim(), password)))
    } else if (mode === 'register') {
      run(async () => {
        const r = await register({
          email: email.trim(),
          username: username.trim(),
          display_name: displayName.trim(),
          password,
          invite: inviteToken ?? undefined,
        })
        if ('pending' in r) {
          switchMode('login')
          setNotice('Registration successful! Your account is awaiting admin approval.')
        } else {
          onAuthenticated(r)
        }
      })
    } else if (mode === 'forgot') {
      run(async () => {
        await forgotPassword(email.trim())
        setNotice('If a confirmed account exists with this address, we sent a link to it.')
      })
    } else if (mode === 'reset' && resetToken) {
      run(async () => {
        await resetPassword(resetToken, password)
        setResetToken(null)
        switchMode('login')
        setNotice('New password set — you can sign in now.')
      })
    }
  }

  const canRegister = !!cfg && (cfg.registration !== 'closed' || !!inviteToken)
  const inviteOnly = cfg?.registration === 'invite' && !inviteToken

  const titles: Record<Mode, string> = {
    login: 'Sign in',
    register: inviteToken ? 'Sign up with invite' : 'Sign up',
    forgot: 'Forgot password',
    reset: 'Set a new password',
  }

  return (
    <div className="centered">
      <form className="card auth-card" onSubmit={submit}>
        <h1>Nook</h1>
        <p className="auth-subtitle">{titles[mode]}</p>
        {notice && <p className="notice">{notice}</p>}

        {mode === 'login' && (
          <>
            <label>
              Username or email
              <input value={loginName} onChange={(e) => setLoginName(e.target.value)} autoComplete="username" autoFocus />
            </label>
            <label>
              Password
              <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" />
            </label>
          </>
        )}

        {mode === 'register' &&
          (inviteOnly ? (
            <p className="hint">Signing up requires an invite link from an admin.</p>
          ) : (
            <>
              <label>
                Email address
                <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" autoFocus />
              </label>
              <label>
                Username
                <input
                  value={username}
                  onChange={(e) => setUsername(e.target.value.toLowerCase())}
                  autoComplete="username"
                  placeholder="e.g. jane.doe"
                />
              </label>
              <label>
                Display name
                <input value={displayName} onChange={(e) => setDisplayName(e.target.value)} autoComplete="name" placeholder="e.g. Jane Doe" />
              </label>
              <label>
                Password (min. 8 characters)
                <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
              </label>
              <label>
                Confirm password
                <input type="password" value={password2} onChange={(e) => setPassword2(e.target.value)} autoComplete="new-password" />
              </label>
              {cfg?.registration === 'approval' && !inviteToken && (
                <p className="hint">New accounts are approved by an admin.</p>
              )}
            </>
          ))}

        {mode === 'forgot' &&
          (cfg?.email_enabled ? (
            <label>
              Email address
              <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" autoFocus />
            </label>
          ) : (
            <p className="hint">
              This server has no email sending configured. Ask an admin for a new password link.
            </p>
          ))}

        {mode === 'reset' && (
          <>
            <label>
              New password (min. 8 characters)
              <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" autoFocus />
            </label>
            <label>
              Confirm new password
              <input type="password" value={password2} onChange={(e) => setPassword2(e.target.value)} autoComplete="new-password" />
            </label>
          </>
        )}

        {error && <p className="error">{error}</p>}

        {!(mode === 'register' && inviteOnly) && !(mode === 'forgot' && !cfg?.email_enabled) && (
          <button type="submit" disabled={busy}>
            {{ login: 'Sign in', register: 'Sign up', forgot: 'Send link', reset: 'Save password' }[mode]}
          </button>
        )}

        <div className="auth-links">
          {mode === 'login' ? (
            <>
              <button type="button" className="link-button" onClick={() => switchMode('forgot')}>
                Forgot password
              </button>
              {canRegister && (
                <button type="button" className="link-button" onClick={() => switchMode('register')}>
                  Sign up
                </button>
              )}
            </>
          ) : (
            <button type="button" className="link-button" onClick={() => switchMode('login')}>
              ← Back to sign in
            </button>
          )}
        </div>
      </form>
    </div>
  )
}
