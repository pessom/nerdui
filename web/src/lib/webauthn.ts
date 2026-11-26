// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Passkeys (WebAuthn, #226, ADR-0057): the server sends the JSON form of the options, with
 * binary values as base64url; the browser API takes and returns ArrayBuffers.
 */

type Json = Record<string, unknown>;
export type PasskeyRequestOptions = { publicKey: Json };

/** Browsers offer passkeys only in a secure context and on a host name, not an IP address. */
export function passkeysSupported(): boolean {
  if (typeof window === "undefined" || typeof window.PublicKeyCredential !== "function") {
    return false;
  }
  const host = window.location.hostname;
  const ip = /^[\d.]+$/.test(host) || host.includes(":") || host.startsWith("[");
  return window.isSecureContext && !ip;
}

function fromB64url(s: string): ArrayBuffer {
  const b64 = s
    .replace(/-/g, "+")
    .replace(/_/g, "/")
    .padEnd(Math.ceil(s.length / 4) * 4, "=");
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
}

function toB64url(buf: ArrayBuffer | ArrayBufferView): string {
  const bytes = ArrayBuffer.isView(buf)
    ? new Uint8Array(buf.buffer, buf.byteOffset, buf.byteLength)
    : new Uint8Array(buf);
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function descriptors(list: unknown): PublicKeyCredentialDescriptor[] | undefined {
  if (!Array.isArray(list)) return undefined;
  return (list as Json[]).map((d) => ({
    ...(d as unknown as PublicKeyCredentialDescriptor),
    id: fromB64url(d.id as string),
  }));
}

function credentialJSON(cred: PublicKeyCredential): Json {
  const r = cred.response;
  const response: Json = { clientDataJSON: toB64url(r.clientDataJSON) };
  if ("attestationObject" in r) {
    const a = r as AuthenticatorAttestationResponse;
    response.attestationObject = toB64url(a.attestationObject);
    response.transports = a.getTransports?.() ?? [];
  } else {
    const a = r as AuthenticatorAssertionResponse;
    response.authenticatorData = toB64url(a.authenticatorData);
    response.signature = toB64url(a.signature);
    if (a.userHandle) response.userHandle = toB64url(a.userHandle);
  }
  return {
    id: cred.id,
    rawId: toB64url(cred.rawId),
    type: cred.type,
    response,
    clientExtensionResults: cred.getClientExtensionResults(),
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
  };
}

/** Adds a passkey with the options of POST /auth/passkeys/options. */
export async function createPasskey(options: PasskeyRequestOptions): Promise<Json> {
  const o = options.publicKey;
  const user = o.user as Json;
  const publicKey = {
    ...o,
    challenge: fromB64url(o.challenge as string),
    user: { ...user, id: fromB64url(user.id as string) },
    excludeCredentials: descriptors(o.excludeCredentials),
  } as unknown as PublicKeyCredentialCreationOptions;
  const cred = await navigator.credentials.create({ publicKey });
  if (!cred) throw new Error("No passkey was created.");
  return credentialJSON(cred as PublicKeyCredential);
}

/** Answers the options of a sign-in with a passkey. */
export async function getPasskey(options: PasskeyRequestOptions): Promise<Json> {
  const o = options.publicKey;
  const publicKey = {
    ...o,
    challenge: fromB64url(o.challenge as string),
    allowCredentials: descriptors(o.allowCredentials),
  } as unknown as PublicKeyCredentialRequestOptions;
  const cred = await navigator.credentials.get({ publicKey });
  if (!cred) throw new Error("No passkey was chosen.");
  return credentialJSON(cred as PublicKeyCredential);
}

/** A readable message; the browser's NotAllowedError covers cancelling and timeouts. */
export function passkeyErrorText(err: unknown): string {
  if (err instanceof DOMException) {
    if (err.name === "NotAllowedError") return "The passkey request was cancelled or timed out.";
    if (err.name === "InvalidStateError")
      return "This authenticator already has a passkey for nerdui.";
    if (err.name === "SecurityError") {
      return "The browser refused the passkey: open nerdui by its host name over HTTPS.";
    }
  }
  return err instanceof Error ? err.message : "The passkey request failed.";
}
