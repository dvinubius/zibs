# zibs frontend

## Purpose

zibs provides a small, public, same-origin interface for creating short links. It is deliberately a vanilla HTML, CSS, and JavaScript page rather than an SPA.

## Visual identity

The page follows the Dinu Barbu design system: Space Grotesk and Azeret Mono webfonts, brand tokens, a `zibs.` type wordmark, and a `z.` favicon. Dark is the default theme — the OS `prefers-color-scheme` is not consulted — and the header toggle stores a visitor's own choice under the `localStorage` key `theme`, applied by an inline script in `<head>` before first paint. The footer carries the personal wordmark and links to `dinubarbu.com` and GitHub.

## Delivery

The page and its assets are embedded in the Go binary from `web/`:

- `GET /` serves the creation page;
- `GET /static/{file}` serves its CSS, JavaScript, fonts, and favicon.

The static-file handler rejects paths ending in `/` to prevent directory listings. The root page route is specific enough that `GET /{code}` continues to serve redirects.

## Creation flow and token handling

The visitor provides a destination URL and a personal creation token. The page sends the token only in the `Authorization: Bearer` header for the creation request.

The token is remembered on the device that used it, under the `localStorage` key `zib-token`, so it has to be entered only once. The page writes it after a request the service accepted the token for — a `201`, or a `400` rejecting the destination URL, both of which mean the token was spent and therefore valid — and never after a `401`. No confirmation is asked for.

While a short link is on screen the lede paragraphs are hidden, saved token or not: they explain how to get a token for the form, and the form is not what the visitor is looking at. They disappear, and the token field changes, only once the form has finished fading out — losing them mid-fade shrinks the card and pulls the page up under the animation — so the renders that take them away are deferred by the fade duration read from the stylesheet, while anything that brings them back applies at once. With a token saved, the page hides the lede paragraphs and fills the token field in from storage, disabled, with the hint below it replaced by an aside reading `// using your saved zib token`. Keeping the field rather than removing it makes plain what is being sent and where the token would go; disabling it makes plain that it is already handled. Once three or fewer uses remain, that aside gains a second line, `// 2 uses left`, with the `//` muted like the line above it and the count itself in the accent colour. The count comes from the last response and is kept beside the token under `zib-token-uses-left`, so the warning is on the page before the next request rather than only after it; a `400`, which carries no count, decrements the stored one, since the use was spent regardless. The request body is unchanged either way. Without a saved token, the form is exactly the one that existed before this memory did.

`POST /links` returns `usesLeft` — the uses the token has after this one. When three or fewer remain, the result terminal carries an extra aside under the short URL and copy button, separated from it by a short hairline: `// N uses left with your saved zib token`, with only the count in the accent colour. At zero the note instead says the token is spent and links to `zibs@dinubarbu.com` for a fresh one, and the page deletes the saved token, so the field empties and reopens for typing and the ledes come back; a `401` clears it the same way, since a revoked or exhausted token is of no further use.

Apart from the token and the theme choice, the frontend persists nothing: no cookies, no URL parameters, no analytics, no browser-side history of created links. The generated URL is displayed exactly once and has a copy action. Every `localStorage` access is wrapped in `try`/`catch`, so a browser that blocks site data falls back to asking for the token each time.

Form inputs intentionally have no `name` attributes. If JavaScript is unavailable and a native form submission happens, the token therefore cannot be sent in a form body or URL.

Visitors request personal creation tokens by email at `zibs@dinubarbu.com`. Tokens do not expire but are limited to a fixed number of uses. One token works on any number of devices; each device that uses it saves its own copy.

Every created link expires 90 days after creation. An expired code returns
`404` immediately; the service removes expired rows at startup and then every
24 hours while it runs.

## Observability

Browser-facing route metrics use bounded route labels:

- `/` for the page;
- `/static` for every static asset.

Individual asset names are not metric labels, avoiding unbounded Prometheus time series.

## Tests

Focused handler tests verify that:

- the root page and static assets are served with suitable content types;
- creation inputs have no `name` attributes;
- the page route does not shadow short-link redirects;
- missing assets return `404`;
- static directory listings are blocked.
