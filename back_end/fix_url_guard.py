"""Fix the URL ContainsAny character guard bug in submit_job_uc.go.

The old string ";&|`$<>\\'\"\\n\\r\\t" resolves in Go to contain literal chars
n, r, t because \\n / \\r / \\t are double-backslash sequences (backslash + letter),
not the control characters newline/carriage-return/tab.

The fix replaces it with ";&|`$<>\\'\"\n\r\t" where \n \r \t are single-backslash
Go escape sequences that produce the actual control characters.
"""

path = "internal/usecase/submit_job_uc.go"

with open(path, "rb") as f:
    raw = f.read()

# The broken pattern as it literally appears in the Go source file bytes
old = b'strings.ContainsAny(req.HFUrl, ";&|`$<>\\\\\'\\\"\\\\n\\\\r\\\\t")'
# The fixed pattern - uses actual Go escape sequences \n \r \t (single backslash)
new = b'strings.ContainsAny(req.HFUrl, ";&|`$<>\\\\\\'\\\"\\n\\r\\t")'

if old in raw:
    fixed = raw.replace(old, new, 1)
    with open(path, "wb") as f:
        f.write(fixed)
    print("FIXED: replaced old ContainsAny pattern")
else:
    # Try alternate: maybe stored without the escaped quote
    print("Pattern not found, printing the relevant line for debugging:")
    for i, line in enumerate(raw.split(b'\n')):
        if b'ContainsAny' in line and b'req.HFUrl' in line:
            print(f"Line {i}: {line!r}")
