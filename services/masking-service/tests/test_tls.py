"""The Elasticsearch CA made by elasticsearch-certutil has no keyUsage extension, which Python 3.13+
rejects by default (VERIFY_X509_STRICT). esm_common.tls.ca_context must still connect, and must
still refuse a server whose certificate does not match the host name."""

import datetime
import socket
import ssl
import sys
import threading

import pytest
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import NameOID
from esm_common.tls import ca_context


def _cert(subject: str, issuer_key, issuer_name, key, *, ca: bool, sans=()):  # type: ignore[no-untyped-def]
    now = datetime.datetime.now(datetime.UTC)
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, subject)])
    builder = (x509.CertificateBuilder()
               .subject_name(name)
               .issuer_name(issuer_name or name)
               .public_key(key.public_key())
               .serial_number(x509.random_serial_number())
               .not_valid_before(now - datetime.timedelta(minutes=1))
               .not_valid_after(now + datetime.timedelta(hours=1))
               # Like elasticsearch-certutil: basicConstraints only, no keyUsage on the CA
               .add_extension(x509.BasicConstraints(ca=ca, path_length=None), critical=True))
    if sans:
        alt = x509.SubjectAlternativeName([x509.DNSName(s) for s in sans])
        builder = builder.add_extension(alt, critical=False)
    return builder.sign(issuer_key, hashes.SHA256())


@pytest.fixture
def tls_server(tmp_path):  # type: ignore[no-untyped-def]
    ca_key = ec.generate_private_key(ec.SECP256R1())
    srv_key = ec.generate_private_key(ec.SECP256R1())
    ca = _cert("esm test CA", ca_key, None, ca_key, ca=True)
    srv = _cert("localhost", ca_key, ca.subject, srv_key, ca=False, sans=["localhost"])
    (tmp_path / "ca.crt").write_bytes(ca.public_bytes(serialization.Encoding.PEM))
    (tmp_path / "srv.pem").write_bytes(
        srv.public_bytes(serialization.Encoding.PEM)
        + srv_key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8,
                                serialization.NoEncryption()))
    server_ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    server_ctx.load_cert_chain(tmp_path / "srv.pem")
    listener = socket.create_server(("127.0.0.1", 0))

    def serve() -> None:
        while True:
            try:
                conn, _ = listener.accept()
            except OSError:
                return
            try:
                with server_ctx.wrap_socket(conn, server_side=True):
                    pass
            except (ssl.SSLError, OSError):
                pass

    threading.Thread(target=serve, daemon=True).start()
    yield str(tmp_path / "ca.crt"), listener.getsockname()[1]
    listener.close()


def _connect(ctx: ssl.SSLContext, port: int, host: str) -> None:
    with socket.create_connection(("127.0.0.1", port), timeout=5) as raw:
        with ctx.wrap_socket(raw, server_hostname=host):
            pass


def test_ca_without_key_usage_is_accepted(tls_server) -> None:  # type: ignore[no-untyped-def]
    ca_file, port = tls_server
    _connect(ca_context(ca_file), port, "localhost")


@pytest.mark.skipif(sys.version_info < (3, 13), reason="VERIFY_X509_STRICT is default from 3.13")
def test_default_context_rejects_it(tls_server) -> None:  # type: ignore[no-untyped-def]
    ca_file, port = tls_server
    with pytest.raises(ssl.SSLCertVerificationError):
        _connect(ssl.create_default_context(cafile=ca_file), port, "localhost")


def test_host_name_is_still_verified(tls_server) -> None:  # type: ignore[no-untyped-def]
    ca_file, port = tls_server
    with pytest.raises(ssl.SSLCertVerificationError):
        _connect(ca_context(ca_file), port, "elasticsearch.example")
