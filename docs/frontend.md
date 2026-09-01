# zibs frontend

## Purpose

zibs provides a small, public, same-origin interface for creating short links. It is deliberately a vanilla HTML, CSS, and JavaScript page rather than an SPA.

## Visual identity

The page follows the Dinu Barbu design system: Space Grotesk and Azeret Mono webfonts, brand tokens, a `zibs.` type wordmark, and a `z.` favicon. The footer carries the personal wordmark and links to `dinubarbu.com` and GitHub.

## Delivery

The page and its assets are embedded in the Go binary from `web/`:

- `GET /` serves the creation page;
- `GET /static/{file}` serves its CSS, JavaScript, fonts, and favicon.

The static-file handler rejects paths ending in `/` to prevent directory listings. The root page route is specific enough that `GET /{code}` continues to serve redirects.

## Creation flow and token handling

The visitor provides a destination URL and a personal creation token. The page sends the token only in the `Authorization: Bearer` header for the creation request.

The frontend must not persist the token or generated short URL. In particular, it uses no cookies, `localStorage`, URL parameters, analytics, or browser-side history. The generated URL is displayed exactly once and has a copy action.

Form inputs intentionally have no `name` attributes. If JavaScript is unavailable and a native form submission happens, the token therefore cannot be sent in a form body or URL.

Visitors request personal creation tokens by email at `zibs@dinubarbu.com`. Tokens do not expire but are limited to a fixed number of uses.

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
