# Security policy

## Reporting a vulnerability

Please **do not open a public issue** for security problems. Email
**security@captchano.com** with a description and steps to reproduce. We aim to
acknowledge reports within 2 business days and will keep you updated until the
issue is resolved.

## API keys

Never commit API keys (`cn-…`). Load them from the environment
(`CAPTCHANO_API_KEY`) or a secret manager. If a key is exposed, revoke it
immediately from your [dashboard](https://captchano.com/dashboard/keys) and
create a new one.
