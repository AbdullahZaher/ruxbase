-- W00-08 fixture for an isolated Supabase Self-Hosted database.
-- Run with psql -v ON_ERROR_STOP=1 -v records=10000. A pre-existing
-- fixture table aborts the transaction. No existing data is dropped.
BEGIN;

SET LOCAL search_path = pg_catalog, public;

CREATE TABLE public.w00_benchmark_posts (
    id text PRIMARY KEY DEFAULT substr(md5(random()::text), 1, 15),
    title text NOT NULL DEFAULT '',
    body text NOT NULL DEFAULT '',
    n double precision NOT NULL DEFAULT 0,
    published boolean NOT NULL DEFAULT false
);

CREATE INDEX idx_w00_benchmark_posts_n ON public.w00_benchmark_posts (n);

INSERT INTO public.w00_benchmark_posts (id, title, body, n, published)
SELECT 'b' || lpad(n::text, 14, '0'),
       'Post ' || n,
       repeat('lorem ipsum dolor sit amet ', 10),
       n,
       n % 2 = 0
FROM generate_series(0, :records - 1) AS fixture(n);

ALTER TABLE public.w00_benchmark_posts ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON public.w00_benchmark_posts FROM PUBLIC, anon, authenticated;
GRANT SELECT, INSERT ON public.w00_benchmark_posts TO anon;
CREATE POLICY w00_benchmark_read ON public.w00_benchmark_posts FOR SELECT TO anon USING (true);
CREATE POLICY w00_benchmark_create ON public.w00_benchmark_posts FOR INSERT TO anon WITH CHECK (true);
NOTIFY pgrst, 'reload schema';

COMMIT;
