CREATE TABLE users (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL,
    phone TEXT NULL,
    password_hash TEXT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    email_verified_at TIMESTAMPTZ NULL,
    phone_verified_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT users_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email));
CREATE UNIQUE INDEX users_phone_key ON users (phone) WHERE phone IS NOT NULL;
