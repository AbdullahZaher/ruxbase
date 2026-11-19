-- users (translated)
-- list: superusersOnly | null
-- view: translated | id = @request.auth.id
-- create: translated | 
-- update: translated | id = @request.auth.id
-- delete: translated | id = @request.auth.id
ALTER TABLE "users" ENABLE ROW LEVEL SECURITY;
CREATE POLICY "ruxbase_view" ON "users" AS PERMISSIVE FOR SELECT TO ruxbase_api USING (COALESCE(current_setting('ruxbase.rule', true), '') <> 'list' AND (CASE (SELECT ruxbase_rls.auth() ->> 'collectionId') WHEN '_pb_users_auth_' THEN ("users"."id" = (SELECT ruxbase_rls.auth() ->> 'id')) WHEN 'kpv709sk2lqbqk8' THEN ("users"."id" = (SELECT ruxbase_rls.auth() ->> 'id')) WHEN 'pbc_3142635823' THEN ("users"."id" = (SELECT ruxbase_rls.auth() ->> 'id')) WHEN 'v851q4r790rhknl' THEN ("users"."id" = (SELECT ruxbase_rls.auth() ->> 'id')) ELSE (((("users"."id")::text = '' OR "users"."id" IS NULL))) END));
CREATE POLICY "ruxbase_create" ON "users" AS PERMISSIVE FOR INSERT TO ruxbase_anon, ruxbase_authenticated, ruxbase_api WITH CHECK (TRUE);
CREATE POLICY "ruxbase_update" ON "users" AS PERMISSIVE FOR UPDATE TO ruxbase_anon, ruxbase_authenticated, ruxbase_api USING (CASE (SELECT ruxbase_rls.auth() ->> 'collectionId') WHEN '_pb_users_auth_' THEN ("users"."id" = (SELECT ruxbase_rls.auth() ->> 'id')) WHEN 'kpv709sk2lqbqk8' THEN ("users"."id" = (SELECT ruxbase_rls.auth() ->> 'id')) WHEN 'pbc_3142635823' THEN ("users"."id" = (SELECT ruxbase_rls.auth() ->> 'id')) WHEN 'v851q4r790rhknl' THEN ("users"."id" = (SELECT ruxbase_rls.auth() ->> 'id')) ELSE (((("users"."id")::text = '' OR "users"."id" IS NULL))) END) WITH CHECK (TRUE);
CREATE POLICY "ruxbase_delete" ON "users" AS PERMISSIVE FOR DELETE TO ruxbase_anon, ruxbase_authenticated, ruxbase_api USING (CASE (SELECT ruxbase_rls.auth() ->> 'collectionId') WHEN '_pb_users_auth_' THEN ("users"."id" = (SELECT ruxbase_rls.auth() ->> 'id')) WHEN 'kpv709sk2lqbqk8' THEN ("users"."id" = (SELECT ruxbase_rls.auth() ->> 'id')) WHEN 'pbc_3142635823' THEN ("users"."id" = (SELECT ruxbase_rls.auth() ->> 'id')) WHEN 'v851q4r790rhknl' THEN ("users"."id" = (SELECT ruxbase_rls.auth() ->> 'id')) ELSE (((("users"."id")::text = '' OR "users"."id" IS NULL))) END);
REVOKE ALL ON TABLE "users" FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api;
GRANT SELECT ON TABLE "users" TO ruxbase_api;

-- demo1 (translated)
-- list: superusersOnly | null
-- view: superusersOnly | null
-- create: superusersOnly | null
-- update: superusersOnly | null
-- delete: superusersOnly | null
ALTER TABLE "demo1" ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON TABLE "demo1" FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api;
GRANT SELECT ON TABLE "demo1" TO ruxbase_api;

-- demo2 (translated)
-- list: translated | 
-- view: translated | 
-- create: translated | 
-- update: translated | 
-- delete: translated | 
ALTER TABLE "demo2" ENABLE ROW LEVEL SECURITY;
CREATE POLICY "ruxbase_list" ON "demo2" AS PERMISSIVE FOR SELECT TO ruxbase_anon, ruxbase_authenticated, ruxbase_api USING (COALESCE(current_setting('ruxbase.rule', true), '') <> 'view' AND (TRUE));
CREATE POLICY "ruxbase_view" ON "demo2" AS PERMISSIVE FOR SELECT TO ruxbase_api USING (COALESCE(current_setting('ruxbase.rule', true), '') <> 'list' AND (TRUE));
CREATE POLICY "ruxbase_create" ON "demo2" AS PERMISSIVE FOR INSERT TO ruxbase_anon, ruxbase_authenticated, ruxbase_api WITH CHECK (TRUE);
CREATE POLICY "ruxbase_update" ON "demo2" AS PERMISSIVE FOR UPDATE TO ruxbase_anon, ruxbase_authenticated, ruxbase_api USING (TRUE) WITH CHECK (TRUE);
CREATE POLICY "ruxbase_delete" ON "demo2" AS PERMISSIVE FOR DELETE TO ruxbase_anon, ruxbase_authenticated, ruxbase_api USING (TRUE);
REVOKE ALL ON TABLE "demo2" FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api;
GRANT SELECT ON TABLE "demo2" TO ruxbase_api;
GRANT SELECT ("id", "title", "active", "created", "updated") ON TABLE "demo2" TO ruxbase_anon, ruxbase_authenticated;
GRANT INSERT ("id", "title", "active", "created", "updated") ON TABLE "demo2" TO ruxbase_anon, ruxbase_authenticated;
GRANT UPDATE ("title", "active", "created", "updated") ON TABLE "demo2" TO ruxbase_anon, ruxbase_authenticated;
GRANT DELETE ON TABLE "demo2" TO ruxbase_anon, ruxbase_authenticated;

-- demo3 (translated)
-- list: translated | // example leading comment... @request.auth.id != "" && @request.auth.collectionName != "users" // example trailing comment...
-- view: translated | @request.auth.id != "" && @request.auth.collectionName != "users"
-- create: translated | @request.auth.id != "" && @request.auth.collectionName != "users"
-- update: translated | @request.auth.id != "" && @request.auth.collectionName != "users"
-- delete: translated | @request.auth.id != "" && @request.auth.collectionName != "users"
ALTER TABLE "demo3" ENABLE ROW LEVEL SECURITY;
CREATE POLICY "ruxbase_list" ON "demo3" AS PERMISSIVE FOR SELECT TO ruxbase_anon, ruxbase_authenticated, ruxbase_api USING (COALESCE(current_setting('ruxbase.rule', true), '') <> 'view' AND (CASE (SELECT ruxbase_rls.auth() ->> 'collectionId') WHEN '_pb_users_auth_' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND 'users' IS DISTINCT FROM 'users')) WHEN 'kpv709sk2lqbqk8' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND 'nologin' IS DISTINCT FROM 'users')) WHEN 'pbc_3142635823' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND '_superusers' IS DISTINCT FROM 'users')) WHEN 'v851q4r790rhknl' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND 'clients' IS DISTINCT FROM 'users')) ELSE (('' IS DISTINCT FROM '' AND '' IS DISTINCT FROM ('users')::text)) END));
CREATE POLICY "ruxbase_view" ON "demo3" AS PERMISSIVE FOR SELECT TO ruxbase_api USING (COALESCE(current_setting('ruxbase.rule', true), '') <> 'list' AND (CASE (SELECT ruxbase_rls.auth() ->> 'collectionId') WHEN '_pb_users_auth_' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND 'users' IS DISTINCT FROM 'users')) WHEN 'kpv709sk2lqbqk8' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND 'nologin' IS DISTINCT FROM 'users')) WHEN 'pbc_3142635823' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND '_superusers' IS DISTINCT FROM 'users')) WHEN 'v851q4r790rhknl' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND 'clients' IS DISTINCT FROM 'users')) ELSE (('' IS DISTINCT FROM '' AND '' IS DISTINCT FROM ('users')::text)) END));
CREATE POLICY "ruxbase_create" ON "demo3" AS PERMISSIVE FOR INSERT TO ruxbase_anon, ruxbase_authenticated, ruxbase_api WITH CHECK (CASE (SELECT ruxbase_rls.auth() ->> 'collectionId') WHEN '_pb_users_auth_' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND 'users' IS DISTINCT FROM 'users')) WHEN 'kpv709sk2lqbqk8' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND 'nologin' IS DISTINCT FROM 'users')) WHEN 'pbc_3142635823' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND '_superusers' IS DISTINCT FROM 'users')) WHEN 'v851q4r790rhknl' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND 'clients' IS DISTINCT FROM 'users')) ELSE (('' IS DISTINCT FROM '' AND '' IS DISTINCT FROM ('users')::text)) END);
CREATE POLICY "ruxbase_update" ON "demo3" AS PERMISSIVE FOR UPDATE TO ruxbase_anon, ruxbase_authenticated, ruxbase_api USING (CASE (SELECT ruxbase_rls.auth() ->> 'collectionId') WHEN '_pb_users_auth_' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND 'users' IS DISTINCT FROM 'users')) WHEN 'kpv709sk2lqbqk8' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND 'nologin' IS DISTINCT FROM 'users')) WHEN 'pbc_3142635823' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND '_superusers' IS DISTINCT FROM 'users')) WHEN 'v851q4r790rhknl' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND 'clients' IS DISTINCT FROM 'users')) ELSE (('' IS DISTINCT FROM '' AND '' IS DISTINCT FROM ('users')::text)) END) WITH CHECK (TRUE);
CREATE POLICY "ruxbase_delete" ON "demo3" AS PERMISSIVE FOR DELETE TO ruxbase_anon, ruxbase_authenticated, ruxbase_api USING (CASE (SELECT ruxbase_rls.auth() ->> 'collectionId') WHEN '_pb_users_auth_' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND 'users' IS DISTINCT FROM 'users')) WHEN 'kpv709sk2lqbqk8' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND 'nologin' IS DISTINCT FROM 'users')) WHEN 'pbc_3142635823' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND '_superusers' IS DISTINCT FROM 'users')) WHEN 'v851q4r790rhknl' THEN ((((SELECT ruxbase_rls.auth() ->> 'id'))::text IS DISTINCT FROM '' AND 'clients' IS DISTINCT FROM 'users')) ELSE (('' IS DISTINCT FROM '' AND '' IS DISTINCT FROM ('users')::text)) END);
REVOKE ALL ON TABLE "demo3" FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api;
GRANT SELECT ON TABLE "demo3" TO ruxbase_api;
GRANT SELECT ("id", "title", "files", "created", "updated") ON TABLE "demo3" TO ruxbase_anon, ruxbase_authenticated;
GRANT INSERT ("id", "title", "files", "created", "updated") ON TABLE "demo3" TO ruxbase_anon, ruxbase_authenticated;
GRANT UPDATE ("title", "files", "created", "updated") ON TABLE "demo3" TO ruxbase_anon, ruxbase_authenticated;
GRANT DELETE ON TABLE "demo3" TO ruxbase_anon, ruxbase_authenticated;

-- demo4 (translated)
-- list: translated | 
-- view: translated | 
-- create: translated | @request.auth.collectionName = 'users'
-- update: translated | @request.auth.collectionName = 'users'
-- delete: translated | 
ALTER TABLE "demo4" ENABLE ROW LEVEL SECURITY;
CREATE POLICY "ruxbase_list" ON "demo4" AS PERMISSIVE FOR SELECT TO ruxbase_anon, ruxbase_authenticated, ruxbase_api USING (COALESCE(current_setting('ruxbase.rule', true), '') <> 'view' AND (TRUE));
CREATE POLICY "ruxbase_view" ON "demo4" AS PERMISSIVE FOR SELECT TO ruxbase_api USING (COALESCE(current_setting('ruxbase.rule', true), '') <> 'list' AND (TRUE));
CREATE POLICY "ruxbase_create" ON "demo4" AS PERMISSIVE FOR INSERT TO ruxbase_anon, ruxbase_authenticated, ruxbase_api WITH CHECK (CASE (SELECT ruxbase_rls.auth() ->> 'collectionId') WHEN '_pb_users_auth_' THEN ('users' = 'users') WHEN 'kpv709sk2lqbqk8' THEN ('nologin' = 'users') WHEN 'pbc_3142635823' THEN ('_superusers' = 'users') WHEN 'v851q4r790rhknl' THEN ('clients' = 'users') ELSE ('' = ('users')::text) END);
CREATE POLICY "ruxbase_update" ON "demo4" AS PERMISSIVE FOR UPDATE TO ruxbase_anon, ruxbase_authenticated, ruxbase_api USING (CASE (SELECT ruxbase_rls.auth() ->> 'collectionId') WHEN '_pb_users_auth_' THEN ('users' = 'users') WHEN 'kpv709sk2lqbqk8' THEN ('nologin' = 'users') WHEN 'pbc_3142635823' THEN ('_superusers' = 'users') WHEN 'v851q4r790rhknl' THEN ('clients' = 'users') ELSE ('' = ('users')::text) END) WITH CHECK (TRUE);
CREATE POLICY "ruxbase_delete" ON "demo4" AS PERMISSIVE FOR DELETE TO ruxbase_anon, ruxbase_authenticated, ruxbase_api USING (TRUE);
REVOKE ALL ON TABLE "demo4" FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api;
GRANT SELECT ON TABLE "demo4" TO ruxbase_api;
GRANT SELECT ("id", "title", "rel_one_no_cascade", "rel_one_no_cascade_required", "rel_one_cascade", "rel_many_no_cascade", "rel_many_no_cascade_required", "rel_many_cascade", "rel_one_unique", "rel_many_unique", "self_rel_one", "self_rel_many", "json_array", "json_object", "created", "updated") ON TABLE "demo4" TO ruxbase_anon, ruxbase_authenticated;
GRANT INSERT ("id", "title", "rel_one_no_cascade", "rel_one_no_cascade_required", "rel_one_cascade", "rel_many_no_cascade", "rel_many_no_cascade_required", "rel_many_cascade", "rel_one_unique", "rel_many_unique", "self_rel_one", "self_rel_many", "json_array", "json_object", "created", "updated") ON TABLE "demo4" TO ruxbase_anon, ruxbase_authenticated;
GRANT UPDATE ("title", "rel_one_no_cascade", "rel_one_no_cascade_required", "rel_one_cascade", "rel_many_no_cascade", "rel_many_no_cascade_required", "rel_many_cascade", "rel_one_unique", "rel_many_unique", "self_rel_one", "self_rel_many", "json_array", "json_object", "created", "updated") ON TABLE "demo4" TO ruxbase_anon, ruxbase_authenticated;
GRANT DELETE ON TABLE "demo4" TO ruxbase_anon, ruxbase_authenticated;

-- clients (translated)
-- list: superusersOnly | null
-- view: superusersOnly | null
-- create: superusersOnly | null
-- update: superusersOnly | null
-- delete: superusersOnly | null
ALTER TABLE "clients" ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON TABLE "clients" FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api;
GRANT SELECT ON TABLE "clients" TO ruxbase_api;

-- nologin (excluded)
-- list: notApplicable | 
-- view: notApplicable | 
-- create: notApplicable | 
-- update: notApplicable | 
-- delete: notApplicable | 
ALTER TABLE "nologin" ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON TABLE "nologin" FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api;

-- demo5 (partial)
-- list: translated | select_many:length = 3
-- view: translated (function) | rel_many.self_rel_many.rel_many_cascade.files:length = 1
-- create: denied | @request.body.total = 3 | "@request.body.total" is not available outside of the API
-- update: denied | @request.body.total = 3 | "@request.body.total" is not available outside of the API
-- delete: denied | @request.query.test:isset = true | "@request.query.test:isset" is not available outside of the API
ALTER TABLE "demo5" ENABLE ROW LEVEL SECURITY;
CREATE POLICY "ruxbase_list" ON "demo5" AS PERMISSIVE FOR SELECT TO ruxbase_anon, ruxbase_authenticated, ruxbase_api USING (COALESCE(current_setting('ruxbase.rule', true), '') <> 'view' AND (ruxbase_json_array_length("demo5"."select_many") = '3'::double precision));
CREATE FUNCTION "ruxbase_rls"."rls_9n89pl5vkct6330_view"(text) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, "public", pg_temp AS $rls$
SELECT EXISTS (SELECT DISTINCT (1) FROM "demo5" LEFT JOIN jsonb_array_elements_text(ruxbase_json_array("demo5"."rel_many")) "__je_demo5_rel_many" ON TRUE LEFT JOIN "demo4" "demo5_rel_many" ON "demo5_rel_many"."id" = "__je_demo5_rel_many"."value" LEFT JOIN jsonb_array_elements_text(ruxbase_json_array("demo5_rel_many"."self_rel_many")) "__je_demo5_rel_many_self_rel_many" ON TRUE LEFT JOIN "demo4" "demo5_rel_many_self_rel_many" ON "demo5_rel_many_self_rel_many"."id" = "__je_demo5_rel_many_self_rel_many"."value" LEFT JOIN jsonb_array_elements_text(ruxbase_json_array("demo5_rel_many_self_rel_many"."rel_many_cascade")) "__je_demo5_rel_many_self_rel_many_rel_many_cascade" ON TRUE LEFT JOIN "demo3" "demo5_rel_many_self_rel_many_rel_many_cascade" ON "demo5_rel_many_self_rel_many_rel_many_cascade"."id" = "__je_demo5_rel_many_self_rel_many_rel_many_cascade"."value" WHERE ((((ruxbase_json_array_length("demo5_rel_many_self_rel_many_rel_many_cascade"."files") = '1'::double precision) AND (NOT EXISTS (SELECT 1 FROM (SELECT ruxbase_json_array_length("__mm_demo5_rel_many_self_rel_many_rel_many_cascade"."files") as "multiMatchValue" FROM "demo5" "__mm_demo5" LEFT JOIN jsonb_array_elements_text(ruxbase_json_array("__mm_demo5"."rel_many")) "__mm_demo5_rel_many_je" ON TRUE LEFT JOIN "demo4" "__mm_demo5_rel_many" ON "__mm_demo5_rel_many"."id" = "__mm_demo5_rel_many_je"."value" LEFT JOIN jsonb_array_elements_text(ruxbase_json_array("__mm_demo5_rel_many"."self_rel_many")) "__mm_demo5_rel_many_self_rel_many_je" ON TRUE LEFT JOIN "demo4" "__mm_demo5_rel_many_self_rel_many" ON "__mm_demo5_rel_many_self_rel_many"."id" = "__mm_demo5_rel_many_self_rel_many_je"."value" LEFT JOIN jsonb_array_elements_text(ruxbase_json_array("__mm_demo5_rel_many_self_rel_many"."rel_many_cascade")) "__mm_demo5_rel_many_self_rel_many_rel_many_cascade_je" ON TRUE LEFT JOIN "demo3" "__mm_demo5_rel_many_self_rel_many_rel_many_cascade" ON "__mm_demo5_rel_many_self_rel_many_rel_many_cascade"."id" = "__mm_demo5_rel_many_self_rel_many_rel_many_cascade_je"."value" WHERE "__mm_demo5"."id" = "demo5"."id") "__sm1" WHERE NOT ("__sm1"."multiMatchValue" = '1'::double precision)))))) AND ("demo5"."id" = $1))
$rls$;
REVOKE ALL ON FUNCTION "ruxbase_rls"."rls_9n89pl5vkct6330_view"(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION "ruxbase_rls"."rls_9n89pl5vkct6330_view"(text) TO ruxbase_anon, ruxbase_authenticated, ruxbase_api;
CREATE POLICY "ruxbase_view" ON "demo5" AS PERMISSIVE FOR SELECT TO ruxbase_api USING (COALESCE(current_setting('ruxbase.rule', true), '') <> 'list' AND ("ruxbase_rls"."rls_9n89pl5vkct6330_view"("demo5"."id")));
CREATE POLICY "ruxbase_create" ON "demo5" AS PERMISSIVE FOR INSERT TO ruxbase_anon, ruxbase_authenticated, ruxbase_api WITH CHECK (FALSE);
CREATE POLICY "ruxbase_update" ON "demo5" AS PERMISSIVE FOR UPDATE TO ruxbase_anon, ruxbase_authenticated, ruxbase_api USING (FALSE) WITH CHECK (TRUE);
CREATE POLICY "ruxbase_delete" ON "demo5" AS PERMISSIVE FOR DELETE TO ruxbase_anon, ruxbase_authenticated, ruxbase_api USING (FALSE);
REVOKE ALL ON TABLE "demo5" FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api;
GRANT SELECT ON TABLE "demo5" TO ruxbase_api;
GRANT SELECT ("id", "select_one", "select_many", "rel_one", "rel_many", "total", "file", "created", "updated") ON TABLE "demo5" TO ruxbase_anon, ruxbase_authenticated;
GRANT INSERT ("id", "select_one", "select_many", "rel_one", "rel_many", "total", "file", "created", "updated") ON TABLE "demo5" TO ruxbase_anon, ruxbase_authenticated;
GRANT UPDATE ("select_one", "select_many", "rel_one", "rel_many", "total", "file", "created", "updated") ON TABLE "demo5" TO ruxbase_anon, ruxbase_authenticated;
GRANT DELETE ON TABLE "demo5" TO ruxbase_anon, ruxbase_authenticated;

-- view1 (excluded)
-- list: notApplicable | @request.auth.id != "" && bool = true
-- view: notApplicable | @request.auth.id != "" && bool = true
-- create: notApplicable | null
-- update: notApplicable | null
-- delete: notApplicable | null
REVOKE ALL ON TABLE "view1" FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api;
GRANT SELECT ON TABLE "view1" TO ruxbase_api;

-- view2 (excluded)
-- list: notApplicable | 
-- view: notApplicable | 
-- create: notApplicable | null
-- update: notApplicable | null
-- delete: notApplicable | null
REVOKE ALL ON TABLE "view2" FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api;
GRANT SELECT ON TABLE "view2" TO ruxbase_api;

-- numeric_id_view (excluded)
-- list: notApplicable | 
-- view: notApplicable | 
-- create: notApplicable | null
-- update: notApplicable | null
-- delete: notApplicable | null
REVOKE ALL ON TABLE "numeric_id_view" FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api;
GRANT SELECT ON TABLE "numeric_id_view" TO ruxbase_api;

-- _superusers (excluded)
-- list: notApplicable | null
-- view: notApplicable | null
-- create: notApplicable | null
-- update: notApplicable | null
-- delete: notApplicable | null
ALTER TABLE "_superusers" ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON TABLE "_superusers" FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api;

-- _externalAuths (excluded)
-- list: notApplicable | @request.auth.id != '' && recordRef = @request.auth.id && collectionRef = @request.auth.collectionId
-- view: notApplicable | @request.auth.id != '' && recordRef = @request.auth.id && collectionRef = @request.auth.collectionId
-- create: notApplicable | null
-- update: notApplicable | null
-- delete: notApplicable | @request.auth.id != '' && recordRef = @request.auth.id && collectionRef = @request.auth.collectionId
ALTER TABLE "_externalAuths" ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON TABLE "_externalAuths" FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api;

-- _mfas (excluded)
-- list: notApplicable | @request.auth.id != '' && recordRef = @request.auth.id && collectionRef = @request.auth.collectionId
-- view: notApplicable | @request.auth.id != '' && recordRef = @request.auth.id && collectionRef = @request.auth.collectionId
-- create: notApplicable | null
-- update: notApplicable | null
-- delete: notApplicable | null
ALTER TABLE "_mfas" ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON TABLE "_mfas" FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api;

-- _otps (excluded)
-- list: notApplicable | @request.auth.id != '' && recordRef = @request.auth.id && collectionRef = @request.auth.collectionId
-- view: notApplicable | @request.auth.id != '' && recordRef = @request.auth.id && collectionRef = @request.auth.collectionId
-- create: notApplicable | null
-- update: notApplicable | null
-- delete: notApplicable | null
ALTER TABLE "_otps" ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON TABLE "_otps" FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api;

-- _authOrigins (excluded)
-- list: notApplicable | @request.auth.id != '' && recordRef = @request.auth.id && collectionRef = @request.auth.collectionId
-- view: notApplicable | @request.auth.id != '' && recordRef = @request.auth.id && collectionRef = @request.auth.collectionId
-- create: notApplicable | null
-- update: notApplicable | null
-- delete: notApplicable | @request.auth.id != '' && recordRef = @request.auth.id && collectionRef = @request.auth.collectionId
ALTER TABLE "_authOrigins" ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON TABLE "_authOrigins" FROM ruxbase_anon, ruxbase_authenticated, ruxbase_api;

