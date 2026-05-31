# Requirements
- Secure multi tenancy. Connections and data should not leak between different BE services
- Clients connect with websockets, and the BE should be able to push data to clients without them having to poll
- Backends only interract with this server over standard http, and the server should be able to push data to backends without them having to poll
- The server should be able to handle a large number of clients and backends concurrently
- Language: golang
- Compatability with websocket.io
- Can we make the bridge service stateless? (apart from connections)

Flow:
1. BE registers with bridge
  2. Input:
    - Public key / jwks endpoint for verifying jwt signed by the BE
    - Endpoint for receiving messages from the bridge
  3. Output
    - JWR 
2. Client connects to bridge using jwt signed by the BE, which contains the BE's id, verified by the bridge using the BE's public key (registered in step 1, or fetched from a well known endpoint on the BE configured when registering)
3. Client messages are forward to the BE by the bridge (To endpoint BE configured when registering)
4. BE POST messages to the bridge, which forwards them to the client

Bridge:
- config:
  - issuer_allowlist: []url
  - deployment_identifier: string
Backend:
- config:
  - bridge_endpoint: url

Client GET /bridge/ws?token=jwt(be_endpoint)

Client GET /BE/listen => {sse_url: /bridge/sse?token
- BE generates a signed JWT with bridge as the audience, and backend jwks issuer, connection id as the subject., and callback_url as a claim.
Client ANY /bridge/sse?token
- Bridge verifies the JWT against public key from the BE jwks endpoint.
Bridge POST /{iss}/connected { send_token, connection_token } => OK
- Messages must be signed by the bridge, be fetches the bridge public key from well-known jwks endpoint on bridge.
Backend POST /bridge/event { send_token, data } => OK

Backend -> Bridge -> Client


Client token format:
```json
{
  "aud": "unique bridge deployment identifer",
  "iss": "be", // Full URL to the BE's callback route
  "exp": timestamp,
  "nbf": timestamp,
  "iat": timestamp,
}
```

/{iss}/.well-known/jwks.json
/{iss} Cloud events endpoint (connected, recieved (ws only), disconnected)

/{bridge_url}/.well-known/jwks.json
/{bridge_url}/send

Send token format:
```json
{
  "sub": "bridge internal connection identifier"
  "aud": "unique bridge deployment identifer",
  "iss": "unique bridge deployment identifer",
  "exp": timestamp,
  "nbf": timestamp,
  "iat": timestamp,
}
```

Inspiration:
- https://github.com/fermyon/websocket-bridge
