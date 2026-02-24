$ORIGIN example.com.
$TTL 300

; SOA record
@   IN  SOA ns1.example.com. admin.example.com. (
            1       ; serial
            3600    ; refresh
            1800    ; retry
            604800  ; expire
            300     ; minimum TTL
        )

; Name server
@   IN  NS  ns1.example.com.
ns1 IN  A   127.0.0.1

; manual.example.com is a manually-managed record with no ownership TXT.
; external-dns-docker must NOT delete this record during reconciliation.
manual  IN  A   10.0.0.1
