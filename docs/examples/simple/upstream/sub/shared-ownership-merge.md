[comment]: <> (::gitspork::begin-upstream-owned-block)
# Header for a file that is managed by upstream

intention in a file like this is that the downstream could inject things b/w header and footer. They could also
put things above this header or below the footer technically, but an upstream maintainer might provide additional
instruction in a file like this via a comment or otherwise to help guide the downstream user.

## A header subsection managed by the upstream
to be injected into downstreams

[comment]: <> (::gitspork::end-upstream-owned-block)

[comment]: <> (::gitspork::begin-upstream-owned-block)
# Footer for a file that is managed by upstream
[comment]: <> (::gitspork::end-upstream-owned-block)

