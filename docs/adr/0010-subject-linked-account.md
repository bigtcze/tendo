# ADR 0010: Optional linked account for person subjects

Status: accepted

A subject may optionally reference a household member account for descriptive association only; the link never grants access or changes memberships or sessions. Only person subjects may be linked, enforced by the database CHECK `subjects_linked_user_person_only`. A composite membership FK uses `ON DELETE NO ACTION`, so future member-removal behavior must clear links with an explicit versioned change. Links are not unique: one account may be associated with multiple subjects. Create idempotency fingerprints append the lowercase linked UUID only when present, preserving the existing unlinked v1 fingerprint bytes for replay compatibility.
