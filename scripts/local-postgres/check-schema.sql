-- Run only through check-schema.sh as the actual runtime login.
-- All rows roll back. Identity sequence allocations are not transactional.
\set ON_ERROR_STOP on
BEGIN;
SET LOCAL plpgsql.check_asserts = on;
SET LOCAL statement_timeout = '30s';
SET LOCAL lock_timeout = '5s';

DO $$
DECLARE
    owner_a bigint;
    owner_b bigint;
    inserted_id bigint;
    pair record;
    test record;
    column_name text;
    value_text text;
    observed numeric;
    observed_json jsonb;
    observed_text text;
    statement text;
    actual_state text;
    negative_count integer := 0;
    identity_count integer := 0;
    long_claim text;
    amount_columns text[] := ARRAY[
        'income', 'investments', 'groceries', 'restaurants', 'utilities',
        'transport', 'household', 'health', 'personal_care', 'clothing',
        'subscriptions', 'education', 'tech', 'culture', 'entertainment', 'gifts', 'travel'
    ];
    binding jsonb := $binding${
      "apiMajor": "v1", "method": "PUT", "month": "2026-10", "generation": "synthetic-generation_1",
      "body": {"expectedVersion": null, "currency": "RUB",
        "income": {"amount": "999999999999999999.999999", "currency": "RUB"},
        "spending": {"groceries": {"amount": "0", "currency": "RUB"},
          "restaurants": null, "utilities": null, "transport": null, "household": null,
          "health": null, "personal-care": null, "clothing": null, "subscriptions": null,
          "education": null, "tech": null, "culture": null, "entertainment": null,
          "gifts": null, "travel": null},
        "investments": {"amount": "0.000001", "currency": "RUB"}}
    }$binding$::jsonb;
    -- Deliberate whitespace/order and an equivalent escape must survive JSON storage.
    response_text text := $response${
      "month": "2026-10", "version": "1", "currency": "R\u0055B",
      "income": {"amount": "999999999999999999.999999", "currency": "RUB"},
      "spending": {"groceries": {"amount": "0", "currency": "RUB"},
        "restaurants": null, "utilities": null, "transport": null, "household": null,
        "health": null, "personal-care": null, "clothing": null, "subscriptions": null,
        "education": null, "tech": null, "culture": null, "entertainment": null,
        "gifts": null, "travel": null},
      "investments": {"amount": "0.000001", "currency": "RUB"},
      "totals": {"income": {"amount": "999999999999999999.999999", "currency": "RUB"},
        "spending": {"amount": "0", "currency": "RUB"},
        "investments": {"amount": "0.000001", "currency": "RUB"},
        "remaining": {"amount": "999999999999999999.999998", "currency": "RUB"}}
    }$response$;
    receipt jsonb;
    candidate jsonb;
    receipt_columns text[] := ARRAY[
        'owner_id', 'idempotency_key', 'key_digest', 'request_binding',
        'response_status', 'response_body', 'response_generation', 'creation_location'
    ];
    receipt_insert text;
BEGIN
    ASSERT session_user = 'budget_runtime' AND current_user = 'budget_runtime', 'Actual runtime login required';
    ASSERT current_database() = 'budget_local' AND current_setting('server_version_num') = '180006', 'Wrong local database/version';
    ASSERT NOT EXISTS (SELECT FROM pg_roles WHERE rolname = current_user
        AND (rolsuper OR rolinherit OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls)), 'Unexpected runtime capabilities';
    ASSERT NOT EXISTS (SELECT FROM pg_auth_members WHERE member = current_user::regrole), 'Unexpected runtime membership';

    -- Exactly the expected columns: nullable unconstrained amounts, no totals/defaults.
    ASSERT (SELECT array_agg(attname::text ORDER BY attnum) FROM pg_attribute
        WHERE attrelid = 'public.monthly_budgets'::regclass AND attnum > 0 AND NOT attisdropped)
        = ARRAY['owner_id', 'month', 'revision', 'currency'] || amount_columns, 'Monthly column mapping changed';
    ASSERT (SELECT count(*) FROM pg_attribute WHERE attrelid = 'public.monthly_budgets'::regclass
        AND attname = ANY(amount_columns) AND atttypid = 'public.budget_source_amount'::regtype
        AND atttypmod = -1 AND NOT attnotnull AND NOT atthasdef) = 17, 'Amount storage/default changed';
    ASSERT (SELECT typbasetype = 'numeric'::regtype AND typtypmod = -1 FROM pg_type
        WHERE oid = 'public.budget_source_amount'::regtype), 'Numeric typmod can round before validation';
    ASSERT (SELECT attidentity = 'a' AND atttypid = 'bigint'::regtype FROM pg_attribute
        WHERE attrelid = 'public.budget_owners'::regclass AND attname = 'id'), 'Owner identity changed';

    -- Incompressible synthetic strings exceed ordinary raw B-tree key limits.
    SELECT string_agg(md5(n::text), '' ORDER BY n) INTO long_claim FROM generate_series(1, 512) n;
    FOR pair IN SELECT * FROM (VALUES
        ('https://issuer.example.test/realm', 'synthetic-user'),
        ('https://other.example.test/realm', 'synthetic-user'),
        ('https://issuer.example.test/realm', 'Synthetic-user'),
        ('https://ISSUER.example.test/realm', 'synthetic-user'),
        ('https://issuer.example.test/realm/', 'synthetic-user'),
        (' https://issuer.example.test/realm', 'synthetic-user'),
        ('https://issuer.example.test/realm', ' synthetic-user '),
        ('https://issuer.example.test/realm', U&'caf\00e9'),
        ('https://issuer.example.test/realm', U&'cafe\0301'),
        (U&'https://issuer.example.test/caf\00e9', 'synthetic-user'),
        (U&'https://issuer.example.test/cafe\0301', 'synthetic-user'),
        ('https://issuer.example.test/' || long_claim, long_claim)
    ) AS identities(issuer, subject) LOOP
        INSERT INTO public.budget_owners (issuer_bytes, subject_bytes)
            VALUES (convert_to(pair.issuer, 'UTF8'), convert_to(pair.subject, 'UTF8')) RETURNING id INTO inserted_id;
        ASSERT inserted_id > 0, 'Generated owner ID missing';
        ASSERT (SELECT issuer_bytes = convert_to(pair.issuer, 'UTF8')
            AND subject_bytes = convert_to(pair.subject, 'UTF8') FROM public.budget_owners WHERE id = inserted_id), 'Identity bytes changed';
        IF identity_count = 0 THEN owner_a := inserted_id; END IF;
        IF identity_count = 1 THEN owner_b := inserted_id; END IF;
        identity_count := identity_count + 1;
        BEGIN
            INSERT INTO public.budget_owners (issuer_bytes, subject_bytes)
                VALUES (convert_to(pair.issuer, 'UTF8'), convert_to(pair.subject, 'UTF8'));
            RAISE EXCEPTION 'Duplicate identity unexpectedly accepted';
        EXCEPTION WHEN unique_violation THEN negative_count := negative_count + 1;
        END;
    END LOOP;
    FOR test IN SELECT * FROM (VALUES
        ('missing issuer', '23502', 'INSERT INTO public.budget_owners (subject_bytes) VALUES (''subject''::bytea)'),
        ('missing subject', '23502', 'INSERT INTO public.budget_owners (issuer_bytes) VALUES (''issuer''::bytea)'),
        ('null issuer', '23502', 'INSERT INTO public.budget_owners (issuer_bytes, subject_bytes) VALUES (NULL, ''subject''::bytea)'),
        ('null subject', '23502', 'INSERT INTO public.budget_owners (issuer_bytes, subject_bytes) VALUES (''issuer''::bytea, NULL)'),
        ('empty issuer', '23514', 'INSERT INTO public.budget_owners (issuer_bytes, subject_bytes) VALUES (''''::bytea, ''subject''::bytea)'),
        ('empty subject', '23514', 'INSERT INTO public.budget_owners (issuer_bytes, subject_bytes) VALUES (''issuer''::bytea, ''''::bytea)')
    ) AS cases(label, state, sql) LOOP
        BEGIN
            EXECUTE test.sql;
            RAISE EXCEPTION 'Unexpected success: %', test.label;
        EXCEPTION WHEN OTHERS THEN
            GET STACKED DIAGNOSTICS actual_state = RETURNED_SQLSTATE;
            IF actual_state <> test.state THEN RAISE EXCEPTION '%: expected %, got % (%)', test.label, test.state, actual_state, SQLERRM; END IF;
            negative_count := negative_count + 1;
        END;
    END LOOP;
    RAISE NOTICE 'PASS owners: % byte-exact identities, distinctions, long claims, required values and duplicate rejection', identity_count;

    INSERT INTO public.monthly_budgets (owner_id, month, revision, currency) VALUES
        (owner_a, '0001-01', 1, 'USD'), (owner_a, '9999-12', 9223372036854775807, 'EUR'),
        (owner_a, '2026-10', 1, 'RUB'), (owner_b, '2026-10', 1, 'RUB');
    ASSERT (SELECT revision FROM public.monthly_budgets WHERE owner_id = owner_a AND month = '9999-12') = 9223372036854775807, 'Revision lost precision';
    FOR test IN SELECT * FROM (VALUES
        ('duplicate month', owner_a, '2026-10', '1', 'RUB', '23505'),
        ('missing owner', -1::bigint, '2026-10', '1', 'RUB', '23503'),
        ('null owner', NULL::bigint, '2026-10', '1', 'RUB', '23502'),
        ('null month', owner_a, NULL, '1', 'RUB', '23502'),
        ('null revision', owner_a, '2026-11', NULL, 'RUB', '23502'),
        ('null currency', owner_a, '2026-11', '1', NULL, '23502'),
        ('zero revision', owner_a, '2026-11', '0', 'RUB', '23514'),
        ('negative revision', owner_a, '2026-11', '-1', 'RUB', '23514'),
        ('revision overflow', owner_a, '2026-11', '9223372036854775808', 'RUB', '22003'),
        ('unsupported currency', owner_a, '2026-11', '1', 'GBP', '23514'),
        ('lowercase currency', owner_a, '2026-11', '1', 'rub', '23514')
    ) AS cases(label, owner_id, month, revision, currency, state) LOOP
        BEGIN
            INSERT INTO public.monthly_budgets (owner_id, month, revision, currency)
                VALUES (test.owner_id, test.month, test.revision::bigint, test.currency);
            RAISE EXCEPTION 'Unexpected success: %', test.label;
        EXCEPTION WHEN OTHERS THEN
            GET STACKED DIAGNOSTICS actual_state = RETURNED_SQLSTATE;
            IF actual_state <> test.state THEN RAISE EXCEPTION '%: expected %, got % (%)', test.label, test.state, actual_state, SQLERRM; END IF;
            negative_count := negative_count + 1;
        END;
    END LOOP;
    FOREACH value_text IN ARRAY ARRAY['0000-01', '10000-01', '2026-00', '2026-13', '2026-1', '26-01', '2026/01', '２０２６-01', '2026-10 ', E'2026-10\n', ''] LOOP
        BEGIN
            INSERT INTO public.monthly_budgets (owner_id, month, revision, currency) VALUES (owner_a, value_text, 1, 'RUB');
            RAISE EXCEPTION 'Invalid month accepted: %', value_text;
        EXCEPTION WHEN check_violation THEN negative_count := negative_count + 1;
        END;
    END LOOP;
    RAISE NOTICE 'PASS monthly keys: owner FK, uniqueness, ASCII month boundaries, currencies and BIGINT revision bounds';

    FOREACH column_name IN ARRAY amount_columns LOOP
        EXECUTE format('SELECT %I FROM public.monthly_budgets WHERE owner_id = $1 AND month = ''2026-10''', column_name) INTO observed USING owner_a;
        ASSERT observed IS NULL, 'Absent amount must remain NULL: ' || column_name;
        FOREACH value_text IN ARRAY ARRAY['0', '0.000001', '999999999999999999.999999', '1.0000000', NULL] LOOP
            EXECUTE format('UPDATE public.monthly_budgets SET %I = $1::numeric WHERE owner_id = $2 AND month = ''2026-10'' RETURNING %I', column_name, column_name)
                INTO observed USING value_text, owner_a;
            ASSERT observed IS NOT DISTINCT FROM value_text::numeric, 'Amount did not round-trip: ' || column_name;
        END LOOP;
        FOREACH value_text IN ARRAY ARRAY['-0.000001', '-1', '1000000000000000000', '0.0000001', '1.1234567', '999999999999999999.9999991', 'NaN', 'Infinity', '-Infinity'] LOOP
            BEGIN
                EXECUTE format('UPDATE public.monthly_budgets SET %I = $1::numeric WHERE owner_id = $2 AND month = ''2026-10''', column_name)
                    USING value_text, owner_a;
                RAISE EXCEPTION 'Invalid amount accepted: % = %', column_name, value_text;
            EXCEPTION WHEN check_violation THEN negative_count := negative_count + 1;
            END;
        END LOOP;
        EXECUTE format('SELECT %I FROM public.monthly_budgets WHERE owner_id = $1 AND month = ''2026-10''', column_name) INTO observed USING owner_a;
        ASSERT observed IS NULL, 'Failed amount update changed value: ' || column_name;
    END LOOP;
    RAISE NOTICE 'PASS all 17 amounts: NULL/zero, exact boundaries, no rounding, magnitude, precision, NaN and infinities';

    UPDATE public.monthly_budgets SET income = 999999999999999999.999999, groceries = 0, investments = 0.000001
        WHERE owner_id = owner_a AND month = '2026-10';
    INSERT INTO public.budget_save_receipts
        (owner_id, idempotency_key, key_digest, request_binding, response_status, response_body, response_generation, creation_location)
        VALUES (owner_a, 'snapshot-Key', sha256(convert_to('snapshot-Key', 'UTF8')), binding, 201,
            response_text::json, 'synthetic-generation_1', '/api/v1/budgets/2026-10');
    SELECT request_binding, response_body::text INTO STRICT observed_json, observed_text
        FROM public.budget_save_receipts WHERE owner_id = owner_a AND idempotency_key = 'snapshot-Key';
    ASSERT observed_json = binding, 'Complete binding changed';
    ASSERT observed_text = response_text, 'Historical response text changed';
    ASSERT (observed_json #>> '{body,income,amount}') = '999999999999999999.999999', 'Exact money string changed';
    ASSERT (observed_json #> '{body,spending,restaurants}') = 'null'::jsonb, 'JSON null changed';
    -- Change top-level object order/whitespace and use an equivalent decoded escape.
    SELECT json_object_agg(key, value ORDER BY key DESC)::text INTO observed_text FROM jsonb_each(binding);
    ASSERT observed_json = replace(observed_text, 'RUB', 'R\u0055B')::jsonb, 'Equivalent escapes/order must bind equally';
    FOREACH candidate IN ARRAY ARRAY[
        jsonb_set(binding, '{body,expectedVersion}', '"1"'),
        jsonb_set(binding, '{body,income,amount}', '"1"'),
        jsonb_set(binding, '{body,spending,restaurants}', '{"amount":"0","currency":"RUB"}'),
        jsonb_set(binding, '{month}', '"2026-11"'),
        jsonb_set(binding, '{generation}', '"synthetic-generation_2"'),
        jsonb_set(binding, '{body,currency}', '"USD"')
    ] LOOP
        ASSERT observed_json <> candidate, 'Changed request binding compared equal';
    END LOOP;
    ASSERT (SELECT delete_after IS NULL FROM public.budget_save_receipts WHERE owner_id = owner_a AND idempotency_key = 'snapshot-Key'), 'Deadline must default to NULL';
    UPDATE public.budget_save_receipts SET delete_after = '2030-02-03 04:05:06.123456+03'::timestamptz
        WHERE owner_id = owner_a AND idempotency_key = 'snapshot-Key';
    ASSERT (SELECT delete_after = '2030-02-03 01:05:06.123456+00'::timestamptz FROM public.budget_save_receipts
        WHERE owner_id = owner_a AND idempotency_key = 'snapshot-Key'), 'Deadline lost instant/microseconds';
    UPDATE public.monthly_budgets SET revision = 2, income = 5 WHERE owner_id = owner_a AND month = '2026-10';
    ASSERT (SELECT revision = 2 AND income = 5 FROM public.monthly_budgets WHERE owner_id = owner_a AND month = '2026-10'), 'Runtime update failed';
    ASSERT (SELECT response_body::text = response_text AND request_binding = binding
        AND response_status = 201 AND response_generation = 'synthetic-generation_1'
        AND creation_location = '/api/v1/budgets/2026-10' FROM public.budget_save_receipts
        WHERE owner_id = owner_a AND idempotency_key = 'snapshot-Key'), 'Budget change rewrote historical receipt';

    receipt := jsonb_build_object('owner_id', owner_a, 'idempotency_key', 'snapshot-Key',
        'key_digest', sha256(convert_to('snapshot-Key', 'UTF8')), 'request_binding', binding,
        'response_status', 201, 'response_body', response_text::json,
        'response_generation', 'synthetic-generation_1', 'creation_location', '/api/v1/budgets/2026-10');
    receipt_insert := format('INSERT INTO public.budget_save_receipts (%s) SELECT %s FROM jsonb_populate_record(NULL::public.budget_save_receipts, $1)',
        array_to_string(receipt_columns, ', '), array_to_string(receipt_columns, ', '));
    -- Owner-wide uniqueness holds even with a different month or generation.
    FOREACH candidate IN ARRAY ARRAY[receipt,
        jsonb_set(receipt, '{request_binding,month}', '"2026-11"'),
        jsonb_set(jsonb_set(receipt, '{request_binding,generation}', '"synthetic-generation_2"'), '{response_generation}', '"synthetic-generation_2"')
    ] LOOP
        BEGIN
            EXECUTE receipt_insert USING candidate;
            RAISE EXCEPTION 'Duplicate owner/key accepted';
        EXCEPTION WHEN unique_violation THEN negative_count := negative_count + 1;
        END;
    END LOOP;
    EXECUTE receipt_insert USING jsonb_set(receipt, '{owner_id}', to_jsonb(owner_b));
    FOREACH value_text IN ARRAY ARRAY['snapshot-key', 'a', repeat('Z', 128), 'AZaz09._:-'] LOOP
        candidate := receipt || jsonb_build_object('idempotency_key', value_text,
            'key_digest', sha256(convert_to(value_text, 'UTF8')), 'response_status', 200,
            'creation_location', NULL, 'response_generation', CASE WHEN value_text = 'a' THEN 'G' ELSE repeat('g', 128) END);
        EXECUTE receipt_insert USING candidate;
    END LOOP;
    ASSERT (SELECT count(*) FROM public.budget_save_receipts) = 6, 'Independent owner/case/key boundary receipts missing';
    ASSERT NOT EXISTS (SELECT FROM public.budget_save_receipts WHERE idempotency_key <> 'snapshot-Key' AND delete_after IS NOT NULL), 'New deadline not NULL';
    FOREACH value_text IN ARRAY ARRAY['', repeat('a', 129), 'bad key', 'bad/key', 'é', E'bad\n', 'bad,key'] LOOP
        candidate := receipt || jsonb_build_object('idempotency_key', value_text, 'key_digest', sha256(convert_to(value_text, 'UTF8')));
        BEGIN
            EXECUTE receipt_insert USING candidate;
            RAISE EXCEPTION 'Invalid key accepted: %', value_text;
        EXCEPTION WHEN check_violation THEN negative_count := negative_count + 1;
        END;
    END LOOP;
    -- Each candidate changes just one otherwise valid receipt field.
    FOR test IN SELECT * FROM (VALUES
        ('digest mismatch', jsonb_build_object('key_digest', sha256('wrong'::bytea)), '23514'),
        ('short digest', jsonb_build_object('key_digest', decode('00', 'hex')), '23514'),
        ('long digest', jsonb_build_object('key_digest', decode(repeat('00', 33), 'hex')), '23514'),
        ('orphan owner', '{"owner_id":-1}'::jsonb, '23503'),
        ('status 199', '{"response_status":199}'::jsonb, '23514'),
        ('status 204', '{"response_status":204}'::jsonb, '23514'),
        ('error status', '{"response_status":500}'::jsonb, '23514'),
        ('creation missing location', '{"creation_location":null}'::jsonb, '23514'),
        ('creation empty location', '{"creation_location":""}'::jsonb, '23514'),
        ('update with location', '{"response_status":200}'::jsonb, '23514'),
        ('empty generation', '{"response_generation":""}'::jsonb, '23514'),
        ('long generation', jsonb_build_object('response_generation', repeat('g', 129)), '23514'),
        ('non-ASCII generation', '{"response_generation":"é"}'::jsonb, '23514'),
        ('generation punctuation', '{"response_generation":"bad.generation"}'::jsonb, '23514'),
        ('generation newline', jsonb_build_object('response_generation', E'bad\n'), '23514')
    ) AS cases(label, patch, state) LOOP
        BEGIN
            EXECUTE receipt_insert USING receipt || test.patch;
            RAISE EXCEPTION 'Unexpected success: %', test.label;
        EXCEPTION WHEN OTHERS THEN
            GET STACKED DIAGNOSTICS actual_state = RETURNED_SQLSTATE;
            IF actual_state <> test.state THEN RAISE EXCEPTION '%: expected %, got % (%)', test.label, test.state, actual_state, SQLERRM; END IF;
            negative_count := negative_count + 1;
        END;
    END LOOP;
    FOREACH column_name IN ARRAY receipt_columns[1:7] LOOP
        statement := format('INSERT INTO public.budget_save_receipts (%s) SELECT %s FROM jsonb_populate_record(NULL::public.budget_save_receipts, $1)',
            array_to_string(array_remove(receipt_columns, column_name), ', '), array_to_string(array_remove(receipt_columns, column_name), ', '));
        BEGIN
            EXECUTE statement USING receipt;
            RAISE EXCEPTION 'Missing required receipt field accepted: %', column_name;
        EXCEPTION WHEN not_null_violation THEN negative_count := negative_count + 1;
        END;
        BEGIN
            EXECUTE receipt_insert USING receipt || jsonb_build_object(column_name, NULL);
            RAISE EXCEPTION 'NULL required receipt field accepted: %', column_name;
        EXCEPTION WHEN not_null_violation THEN negative_count := negative_count + 1;
        END;
    END LOOP;
    FOREACH column_name IN ARRAY ARRAY['request_binding', 'response_body'] LOOP
        FOREACH value_text IN ARRAY ARRAY['null', '[]', '"text"', '1', 'true'] LOOP
            -- SQL NULL above is distinct from JSON null here.
            statement := format('INSERT INTO public.budget_save_receipts (%s) SELECT %s FROM jsonb_populate_record(NULL::public.budget_save_receipts, $1)',
                array_to_string(receipt_columns, ', '),
                array_to_string(ARRAY(SELECT CASE WHEN c = column_name THEN '$2::' || CASE WHEN c = 'request_binding' THEN 'jsonb' ELSE 'json' END ELSE c END FROM unnest(receipt_columns) c), ', '));
            BEGIN
                EXECUTE statement USING receipt, value_text;
                RAISE EXCEPTION 'Non-object JSON accepted: % = %', column_name, value_text;
            EXCEPTION WHEN check_violation THEN negative_count := negative_count + 1;
            END;
        END LOOP;
    END LOOP;
    RAISE NOTICE 'PASS receipts: keys/digests, owner scope, required fields, success metadata, binding equality, exact historical JSON and deadline round-trip';

    -- Actual runtime login: denied actions must fail for privileges (42501).
    FOREACH statement IN ARRAY ARRAY[
        'SET ROLE budget_migrator', 'SET ROLE budget_local',
        'CREATE TABLE public._budget_schema_forbidden (id integer)',
        'CREATE SCHEMA _budget_schema_forbidden',
        'CREATE TEMP TABLE _budget_schema_forbidden (id integer)',
        'ALTER DOMAIN public.budget_source_amount DROP CONSTRAINT budget_source_amount_precision',
        'ALTER SEQUENCE public.budget_owners_id_seq RESTART WITH 1',
        'SELECT setval(''public.budget_owners_id_seq'', 1)',
        'SELECT * FROM public.goose_db_version',
        'INSERT INTO public.goose_db_version (version_id, is_applied) VALUES (2, true)',
        'UPDATE public.goose_db_version SET is_applied = false',
        'DELETE FROM public.goose_db_version',
        'TRUNCATE public.goose_db_version',
        'SELECT nextval(''public.goose_db_version_id_seq'')',
        'INSERT INTO public.budget_owners (id, issuer_bytes, subject_bytes) OVERRIDING SYSTEM VALUE VALUES (-1, ''i''::bytea, ''s''::bytea)',
        'UPDATE public.budget_owners SET issuer_bytes = issuer_bytes',
        'UPDATE public.budget_owners SET subject_bytes = subject_bytes',
        'UPDATE public.budget_owners SET id = DEFAULT',
        'UPDATE public.monthly_budgets SET owner_id = owner_id',
        'UPDATE public.monthly_budgets SET month = month',
        'UPDATE public.monthly_budgets SET currency = currency',
        format('INSERT INTO public.budget_save_receipts (%s, delete_after) SELECT %s, NULL::timestamptz FROM public.budget_save_receipts LIMIT 1', array_to_string(receipt_columns, ', '), array_to_string(receipt_columns, ', '))
    ] LOOP
        BEGIN
            EXECUTE statement;
            RAISE EXCEPTION 'Runtime unexpectedly allowed: %', statement;
        EXCEPTION WHEN insufficient_privilege THEN negative_count := negative_count + 1;
        END;
    END LOOP;
    FOREACH column_name IN ARRAY receipt_columns LOOP
        BEGIN
            EXECUTE format('UPDATE public.budget_save_receipts SET %I = %I', column_name, column_name);
            RAISE EXCEPTION 'Runtime rewrote receipt field: %', column_name;
        EXCEPTION WHEN insufficient_privilege THEN negative_count := negative_count + 1;
        END;
    END LOOP;
    FOREACH column_name IN ARRAY ARRAY['budget_owners', 'monthly_budgets', 'budget_save_receipts'] LOOP
        FOREACH statement IN ARRAY ARRAY[
            format('ALTER TABLE public.%I ADD COLUMN forbidden integer', column_name),
            format('DROP TABLE public.%I', column_name),
            format('TRUNCATE public.%I', column_name),
            format('DELETE FROM public.%I', column_name)
        ] LOOP
            BEGIN
                EXECUTE statement;
                RAISE EXCEPTION 'Runtime unexpectedly allowed: %', statement;
            EXCEPTION WHEN insufficient_privilege THEN negative_count := negative_count + 1;
            END;
        END LOOP;
        ASSERT NOT has_table_privilege(current_user, 'public.' || column_name, 'SELECT WITH GRANT OPTION'), 'Unexpected grant option';
        ASSERT NOT has_any_column_privilege(current_user, 'public.' || column_name, 'INSERT WITH GRANT OPTION,UPDATE WITH GRANT OPTION'), 'Unexpected column grant option';
    END LOOP;
    ASSERT NOT has_schema_privilege(current_user, 'public', 'CREATE'), 'Runtime schema CREATE';
    ASSERT NOT has_database_privilege(current_user, current_database(), 'CREATE,TEMPORARY'), 'Runtime database CREATE/TEMP';
    ASSERT nextval('public.budget_owners_id_seq') > inserted_id, 'Runtime sequence USAGE failed';
    RAISE NOTICE 'PASS runtime: actual-login SELECT/INSERT, permitted updates and identity allocation; DDL, immutable fields, deletes, sequence reset, escalation and Goose access denied';
    RAISE NOTICE 'PASS % expected negative cases with exact SQLSTATE checks', negative_count;
END
$$;
ROLLBACK;
