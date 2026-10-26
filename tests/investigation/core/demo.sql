INSERT INTO v_snapshot VALUES ('snap-demo-001','2026-01-01T00:00:00Z','2026-02-01T00:00:00Z','2026-02-01T00:00:00Z','demo-v1','complete');
INSERT INTO v_components VALUES
 ('cmp-api','API','demo/api','apps/api','application','identified','observed','complete'),
 ('cmp-web','Web','demo/web','app','application','identified','unknown','complete');
INSERT INTO v_issues VALUES
 ('iss-1','UNI-1','linear','Exportar pedidos','done','person-a','team-uni','cmp-api','2026-01-01T00:00:00Z','2026-01-04T00:00:00Z','2026-01-02T00:00:00Z','2026-01-04T00:00:00Z',3,5,1,0,1,172800,'ev-1'),
 ('iss-2','UNI-2','linear','Ajustar formulário','done','person-b','team-uni','cmp-web','2026-01-03T00:00:00Z','2026-01-06T00:00:00Z','2026-01-05T00:00:00Z','2026-01-06T00:00:00Z',2,2,0,0,1,86400,'ev-2'),
 ('iss-3','UNI-3','linear','Comportamento esperado não descrito','open',NULL,'team-uni','cmp-api','2026-01-07T00:00:00Z','2026-01-07T00:00:00Z',NULL,NULL,NULL,NULL,0,0,0,NULL,'ev-3');
INSERT INTO v_pull_requests VALUES
 ('pr-10','demo/api',10,'Correção da exportação','person-a','closed','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z','2026-01-04T00:00:00Z','2026-01-04T01:00:00Z','demo-head-a','demo-merge-a',172800,'ev-4'),
 ('pr-20','demo/web',20,'Ajuste do formulário','person-b','closed','2026-01-05T00:00:00Z','2026-01-05T00:00:00Z','2026-01-05T01:00:00Z','2026-01-06T00:00:00Z','demo-head-b','demo-merge-b',3600,'ev-5');
INSERT INTO v_links VALUES
 ('link-1','iss-1','references_pr','pr-10','direct','ev-1',NULL,NULL),
 ('link-2','iss-2','references_pr','pr-20','direct','ev-2',NULL,NULL),
 ('link-3','pr-10','modifies_component','cmp-api','direct','ev-4',NULL,NULL),
 ('link-4','pr-20','modifies_component','cmp-web','direct','ev-5',NULL,NULL);
INSERT INTO v_events VALUES
 ('evt-1','iss-1','started','2026-01-02T00:00:00Z','2026-01-31T23:00:00Z','person-a','todo','in_progress','ev-1'),
 ('evt-2','iss-1','estimate_changed','2026-01-03T00:00:00Z','2026-01-31T23:00:00Z','person-a','3','5','ev-6'),
 ('evt-3','iss-1','reopened','2026-01-03T00:00:00Z','2026-01-31T23:00:00Z',NULL,'done','open','ev-7'),
 ('evt-4','iss-1','completed','2026-01-04T00:00:00Z','2026-01-31T23:00:00Z','person-a','open','done','ev-1'),
 ('evt-0','iss-1','completed','2026-01-02T12:00:00Z','2026-01-31T23:00:00Z','person-a','in_progress','done','ev-1');
INSERT INTO v_findings VALUES
 ('f-1','R-DELIVERY-001','1.0.0','high','cmp-api','Correção não comprovada no artefato observado','Há correção registrada; sua presença no artefato observado não foi comprovada.','direct','complete'),
 ('f-2','R-LINK-001','1.0.0','medium','iss-3','Sem vínculo explícito localizado','Não foi localizado vínculo explícito com PR no conjunto coletado; isso não demonstra ausência de implementação.','direct','complete');
INSERT INTO v_finding_evidence VALUES ('f-1','ev-4'),('f-1','ev-8'),('f-2','ev-3');
INSERT INTO v_coverage VALUES
 ('github','demo/api','pull_requests','complete',1,1,'','2026-01-31T23:00:00Z'),
 ('github','demo/api','reviews','complete',1,1,'','2026-01-31T23:00:00Z'),
 ('github','demo/web','pull_requests','complete',1,1,'','2026-01-31T23:00:00Z'),
 ('github','demo/web','reviews','complete',1,1,'','2026-01-31T23:00:00Z'),
 ('linear','team-uni','issues','complete',3,3,'','2026-01-31T23:00:00Z'),
 ('runtime','production','import','complete',1,1,'Snapshot operacional fictício importado','2026-01-31T23:00:00Z'),
 ('github','demo/api','actions','not_selected',0,NULL,'Módulo CI não selecionado','2026-01-31T23:00:00Z');
INSERT INTO v_metrics VALUES
 ('metric-1','Entregas observadas',2,'items',2,3,'Issues com completed_at e is_context=0','Não é score de produtividade'),
 ('metric-2','Espera de revisão mediana',88200,'seconds',2,2,'Mediana das esperas observadas','Fixture; não é tempo trabalhado');
INSERT INTO v_stage_times VALUES
 ('pr-10','cmp-api','review_wait','2026-01-02T00:00:00Z','2026-01-04T00:00:00Z',172800,1,'ev-4'),
 ('pr-20','cmp-web','review_wait','2026-01-05T00:00:00Z','2026-01-05T01:00:00Z',3600,1,'ev-5');
INSERT INTO v_estimate_changes VALUES
 ('estimate-change-1','iss-1','person-a','2026-01-03T00:00:00Z',3,5,'after_start','ev-6');
INSERT INTO v_rework VALUES
 ('rework-1','iss-1','cmp-api','reopening','direct','2026-01-03T00:00:00Z','undetermined','ev-7');
INSERT INTO v_delivery_artifacts VALUES
 ('artifact-demo','cmp-api','sha256:demo','demo-old-sha','production','2026-01-30T00:00:00Z','declared','ev-8');
INSERT INTO v_evidence VALUES ('ev-1','fixture','https://example.invalid/evidence/1','2026-01-31T23:00:00Z',NULL,'3cff7e11c4c788cbf4583a636a32f8a9236c67875158181c6ab1715eaa7b6a9c','Evidência fictícia 1; não representa dado real.',0,0);
INSERT INTO v_evidence VALUES ('ev-2','fixture','https://example.invalid/evidence/2','2026-01-31T23:00:00Z',NULL,'23267854f98b6822e482c101a1d7d7931dac630168adc026a38d42fcbc7570ff','Evidência fictícia 2; não representa dado real.',0,0);
INSERT INTO v_evidence VALUES ('ev-3','fixture','https://example.invalid/evidence/3','2026-01-31T23:00:00Z',NULL,'8a156bf37c972dc8f282e1ad6184d81af847c24f8ab0405bcf7cf3297cbee5f6','Evidência fictícia 3; não representa dado real.',0,0);
INSERT INTO v_evidence VALUES ('ev-4','fixture','https://example.invalid/evidence/4','2026-01-31T23:00:00Z',NULL,'9b16d5c44955a97f68e0f6d78fbbf129bbf99f35f2b385e9eef92a8fb8ef1213','Evidência fictícia 4; não representa dado real.',0,0);
INSERT INTO v_evidence VALUES ('ev-5','fixture','https://example.invalid/evidence/5','2026-01-31T23:00:00Z',NULL,'f4efbef4b3e5693b64b9461be4bdb7ff3a906f2d551d67529bd01ceb60b433c4','Evidência fictícia 5; não representa dado real.',0,0);
INSERT INTO v_evidence VALUES ('ev-6','fixture','https://example.invalid/evidence/6','2026-01-31T23:00:00Z',NULL,'333a8981f67344fcf487c7ac992984883ebc655d951f52522dbc4b35cde8778e','Evidência fictícia 6; não representa dado real.',0,0);
INSERT INTO v_evidence VALUES ('ev-7','fixture','https://example.invalid/evidence/7','2026-01-31T23:00:00Z',NULL,'5577e80951b61c30f30c63e1030c8fe0af83b2c7e06a4131cd85a0f5751b2d92','Evidência fictícia 7; não representa dado real.',0,0);
INSERT INTO v_evidence VALUES ('ev-8','fixture','https://example.invalid/evidence/8','2026-01-31T23:00:00Z',NULL,'16fc3d5eccfa393518cff9a1dd4723a287654d7bfe8907d9fb4067e93cadcf1c','Evidência fictícia 8; não representa dado real.',0,0);
