-- +goose Up
-- Goose wraps this file in one transaction. Run as the migration role.
-- Roles and the existing public schema are provisioned separately.

CREATE DOMAIN public.budget_source_amount AS numeric
    CONSTRAINT budget_source_amount_finite CHECK (
        VALUE NOT IN ('NaN'::numeric, 'Infinity'::numeric, '-Infinity'::numeric)
    )
    CONSTRAINT budget_source_amount_range CHECK (
        VALUE >= 0 AND VALUE < 1000000000000000000::numeric
    )
    CONSTRAINT budget_source_amount_precision CHECK (
        VALUE = trunc(VALUE, 6)
    );

CREATE TABLE public.budget_owners (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    issuer_bytes bytea NOT NULL,
    subject_bytes bytea NOT NULL,
    CONSTRAINT budget_owners_issuer_nonempty CHECK (octet_length(issuer_bytes) > 0),
    CONSTRAINT budget_owners_subject_nonempty CHECK (octet_length(subject_bytes) > 0)
);

-- A digest hit requires comparison of BOTH original byte strings by the writer.
CREATE UNIQUE INDEX budget_owners_identity_digest_key
    ON public.budget_owners (sha256(issuer_bytes), sha256(subject_bytes));

CREATE TABLE public.monthly_budgets (
    owner_id bigint NOT NULL,
    month text COLLATE "C" NOT NULL,
    revision bigint NOT NULL,
    currency text COLLATE "C" NOT NULL,
    income public.budget_source_amount,
    investments public.budget_source_amount,
    groceries public.budget_source_amount,
    restaurants public.budget_source_amount,
    utilities public.budget_source_amount,
    transport public.budget_source_amount,
    household public.budget_source_amount,
    health public.budget_source_amount,
    personal_care public.budget_source_amount,
    clothing public.budget_source_amount,
    subscriptions public.budget_source_amount,
    education public.budget_source_amount,
    tech public.budget_source_amount,
    culture public.budget_source_amount,
    entertainment public.budget_source_amount,
    gifts public.budget_source_amount,
    travel public.budget_source_amount,
    CONSTRAINT monthly_budgets_pkey PRIMARY KEY (owner_id, month),
    CONSTRAINT monthly_budgets_owner_fk FOREIGN KEY (owner_id)
        REFERENCES public.budget_owners (id),
    CONSTRAINT monthly_budgets_month_valid CHECK (
        octet_length(month) = 7
        AND month ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'
        AND left(month, 4) <> '0000'
    ),
    CONSTRAINT monthly_budgets_revision_positive CHECK (revision > 0),
    CONSTRAINT monthly_budgets_currency_supported CHECK (currency IN ('RUB', 'USD', 'EUR'))
);

CREATE TABLE public.budget_save_receipts (
    owner_id bigint NOT NULL,
    idempotency_key text COLLATE "C" NOT NULL,
    key_digest bytea NOT NULL,
    request_binding jsonb NOT NULL,
    response_status smallint NOT NULL,
    response_body json NOT NULL,
    response_generation text COLLATE "C" NOT NULL,
    creation_location text COLLATE "C",
    delete_after timestamptz DEFAULT NULL,
    CONSTRAINT budget_save_receipts_pkey PRIMARY KEY (owner_id, key_digest),
    CONSTRAINT budget_save_receipts_owner_fk FOREIGN KEY (owner_id)
        REFERENCES public.budget_owners (id),
    CONSTRAINT budget_save_receipts_key_valid CHECK (
        octet_length(idempotency_key) BETWEEN 1 AND 128
        AND idempotency_key !~ '[^A-Za-z0-9._:-]'
    ),
    CONSTRAINT budget_save_receipts_digest_valid CHECK (
        octet_length(key_digest) = 32
        AND key_digest = sha256(convert_to(idempotency_key, 'UTF8'))
    ),
    -- API validation and complete binding/snapshot construction precede storage.
    -- These object checks also reject JSON null, distinct from SQL NULL.
    CONSTRAINT budget_save_receipts_binding_object CHECK (jsonb_typeof(request_binding) = 'object'),
    CONSTRAINT budget_save_receipts_response_object CHECK (json_typeof(response_body) = 'object'),
    CONSTRAINT budget_save_receipts_status_success CHECK (response_status IN (200, 201)),
    CONSTRAINT budget_save_receipts_generation_valid CHECK (
        octet_length(response_generation) BETWEEN 1 AND 128
        AND response_generation !~ '[^A-Za-z0-9_-]'
    ),
    CONSTRAINT budget_save_receipts_location_by_status CHECK (
        (response_status = 201 AND creation_location IS NOT NULL AND octet_length(creation_location) > 0)
        OR (response_status = 200 AND creation_location IS NULL)
    )
);

-- Named application objects only: no runtime grant on Goose bookkeeping.
GRANT SELECT ON public.budget_owners, public.monthly_budgets, public.budget_save_receipts
    TO budget_runtime;
GRANT INSERT (issuer_bytes, subject_bytes) ON public.budget_owners TO budget_runtime;
GRANT USAGE ON SEQUENCE public.budget_owners_id_seq TO budget_runtime;
GRANT INSERT ON public.monthly_budgets TO budget_runtime;
GRANT UPDATE (
    revision, income, investments, groceries, restaurants, utilities, transport,
    household, health, personal_care, clothing, subscriptions, education, tech,
    culture, entertainment, gifts, travel
) ON public.monthly_budgets TO budget_runtime;
GRANT INSERT (
    owner_id, idempotency_key, key_digest, request_binding, response_status,
    response_body, response_generation, creation_location
) ON public.budget_save_receipts TO budget_runtime;
GRANT UPDATE (delete_after) ON public.budget_save_receipts TO budget_runtime;

-- +goose Down
-- Refuse a destructive or metadata-only downgrade of this initial schema.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'Automatic downgrade of the initial Budget schema is not supported';
END
$$;
-- +goose StatementEnd
