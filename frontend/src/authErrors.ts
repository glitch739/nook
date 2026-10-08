// User-friendly text for the backend's known account-related error
// messages; anything unknown is shown as-is.
const MESSAGES: Record<string, string> = {
  'invalid username or password': 'Incorrect username/email or password.',
  'account awaiting approval': 'Your account is still awaiting admin approval.',
  'username already taken': 'That username is already taken.',
  'email already registered': 'An account with this email already exists.',
  'invalid email address': 'Invalid email address.',
  'username must be 3-32 characters: a-z, 0-9, dot, underscore, hyphen':
    'Username must be 3-32 characters: lowercase letters, digits, dot, underscore, hyphen.',
  'password must be 8-256 characters': 'Password must be at least 8 characters.',
  'registration is closed': 'Registration is currently closed.',
  'an invite is required to register': 'An invite is required to sign up.',
  'invalid or expired invite': 'This invite is invalid or has expired.',
  'invalid or expired link': 'This link is invalid or has expired.',
  'current password is incorrect': 'Current password is incorrect.',
  'cannot remove the last active admin': 'The last active admin cannot be removed.',
  'no such user': 'No such user.',
  'already a member': 'Already a member of this group.',
  'please wait a moment before requesting another email': 'Please wait a moment before requesting another email.',
  'too many requests': 'Too many attempts — please wait a moment.',
}

export function authErrorText(err: unknown): string {
  const raw = err instanceof Error ? err.message : String(err)
  return MESSAGES[raw.trim().toLowerCase()] ?? MESSAGES[raw.trim()] ?? raw
}
