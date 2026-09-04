---
status: accepted
---

# Retain databases on project deletion

Deleting a Supabase project must not cascade into deletion of its database or
durable volumes. A rebuildable project's retained database may be deleted by a
separate explicit operation; a replaced preserved project keeps its old
database read-only for at least 72 hours after traffic moves, so an accidental
custom-resource deletion or a failed cutover does not also destroy the recovery
path.
