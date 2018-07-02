A simple multi-file log writer

This is a client of `memlogd` which writes logs to individual files,
one per service. The writer keeps track of how big each file has become
and performs automatic rotation when the file exceeds a defined limit.
