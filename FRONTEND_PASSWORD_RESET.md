POST /auth/password-reset
Requires a Bearer access token. The body must include both fields:

{
  "old_password": "secret123",
  "new_password": "Str0ng-Passphrase!"
}

Returns 204 on success, 401 invalid_credentials when the old password is wrong.
