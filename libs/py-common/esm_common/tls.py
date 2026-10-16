"""TLS for connections to Elasticsearch."""

from __future__ import annotations

import ssl


def ca_context(ca_file: str | None) -> ssl.SSLContext:
    """A verifying SSL context that trusts ca_file (or the system store).

    Python 3.13+ turns on VERIFY_X509_STRICT, which rejects a CA without the keyUsage extension.
    The CA made by elasticsearch-certutil (compose, bare metal) has none, so every connection to
    Elasticsearch failed. The flag is cleared for that CA only; the chain, expiry and host name are
    still verified.
    """
    ctx = ssl.create_default_context(cafile=ca_file or None)
    ctx.minimum_version = ssl.TLSVersion.TLSv1_2
    if ca_file:
        ctx.verify_flags &= ~ssl.VERIFY_X509_STRICT
    return ctx
