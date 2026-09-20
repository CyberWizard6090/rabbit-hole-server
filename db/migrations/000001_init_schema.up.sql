-- SQLBook: Code
CREATE TABLE public.folders (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    name character varying(255) NOT NULL,
    space_id bigint NOT NULL,
    parent_id bigint
);




CREATE SEQUENCE public.folders_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.folders_id_seq OWNER TO postgres;


ALTER SEQUENCE public.folders_id_seq OWNED BY public.folders.id;



CREATE TABLE public.lists (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    name text NOT NULL,
    space_id bigint NOT NULL,
    folder_id bigint,
    parent_status_id bigint
);




CREATE SEQUENCE public.lists_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.lists_id_seq OWNER TO postgres;


ALTER SEQUENCE public.lists_id_seq OWNED BY public.lists.id;



CREATE TABLE public.permissions (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    code character varying(100) NOT NULL,
    description character varying(255)
);




CREATE SEQUENCE public.permissions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.permissions_id_seq OWNER TO postgres;


ALTER SEQUENCE public.permissions_id_seq OWNED BY public.permissions.id;



CREATE TABLE public.role_permissions (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    role_id bigint NOT NULL,
    permission_id bigint NOT NULL
);




CREATE SEQUENCE public.role_permissions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.role_permissions_id_seq OWNER TO postgres;


ALTER SEQUENCE public.role_permissions_id_seq OWNED BY public.role_permissions.id;



CREATE TABLE public.roles (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    workspace_id bigint NOT NULL,
    name character varying(100) NOT NULL
);




CREATE SEQUENCE public.roles_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.roles_id_seq OWNER TO postgres;


ALTER SEQUENCE public.roles_id_seq OWNED BY public.roles.id;



CREATE TABLE public.spaces (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    name text NOT NULL,
    workspace_id bigint NOT NULL,
    currency text DEFAULT 'USD'::text,
    owner_id bigint NOT NULL
);




CREATE SEQUENCE public.spaces_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.spaces_id_seq OWNER TO postgres;


ALTER SEQUENCE public.spaces_id_seq OWNED BY public.spaces.id;



CREATE TABLE public.tags (
    id bigint NOT NULL,
    space_id bigint NOT NULL,
    name text NOT NULL,
    color character varying(7) NOT NULL
);




CREATE SEQUENCE public.tags_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.tags_id_seq OWNER TO postgres;


ALTER SEQUENCE public.tags_id_seq OWNED BY public.tags.id;



CREATE TABLE public.task_assignees (
    task_id bigint NOT NULL,
    user_id bigint NOT NULL
);




CREATE TABLE public.task_statuses (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    space_id bigint NOT NULL,
    list_id bigint NOT NULL,
    name text NOT NULL,
    color character varying(7) NOT NULL,
    "position" bigint NOT NULL,
    type smallint NOT NULL
);




CREATE SEQUENCE public.task_statuses_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.task_statuses_id_seq OWNER TO postgres;


ALTER SEQUENCE public.task_statuses_id_seq OWNED BY public.task_statuses.id;



CREATE TABLE public.task_tags (
    task_id bigint NOT NULL,
    tag_id bigint NOT NULL
);




CREATE TABLE public.tasks (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    space_id bigint NOT NULL,
    list_id bigint NOT NULL,
    status_id bigint NOT NULL,
    parent_id bigint,
    title text NOT NULL,
    description text,
    priority bigint DEFAULT 2,
    user_id bigint NOT NULL,
    start_date timestamp with time zone,
    due_date timestamp with time zone,
    time_estimate bigint DEFAULT 0,
    time_spent bigint DEFAULT 0
);




CREATE SEQUENCE public.tasks_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.tasks_id_seq OWNER TO postgres;


ALTER SEQUENCE public.tasks_id_seq OWNED BY public.tasks.id;



CREATE TABLE public.user_contacts (
    user_id bigint NOT NULL,
    contact_id bigint NOT NULL
);




CREATE TABLE public.user_sessions (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    token_hash text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone
);




CREATE SEQUENCE public.user_sessions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.user_sessions_id_seq OWNER TO postgres;


ALTER SEQUENCE public.user_sessions_id_seq OWNED BY public.user_sessions.id;



CREATE TABLE public.users (
    id bigint NOT NULL,
    email text NOT NULL,
    password_hash text NOT NULL,
    username text,
    first_name text,
    last_name text,
    avatar_url text,
    bio text,
    time_zone text DEFAULT 'UTC'::text,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);




CREATE SEQUENCE public.users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.users_id_seq OWNER TO postgres;


ALTER SEQUENCE public.users_id_seq OWNED BY public.users.id;



CREATE TABLE public.workspace_members (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    workspace_id bigint NOT NULL,
    user_id bigint NOT NULL,
    role_id bigint NOT NULL
);




CREATE SEQUENCE public.workspace_members_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.workspace_members_id_seq OWNER TO postgres;


ALTER SEQUENCE public.workspace_members_id_seq OWNED BY public.workspace_members.id;



CREATE TABLE public.workspaces (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    name character varying(255) NOT NULL,
    owner_id bigint NOT NULL,
    description text
);




CREATE SEQUENCE public.workspaces_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.workspaces_id_seq OWNER TO postgres;


ALTER SEQUENCE public.workspaces_id_seq OWNED BY public.workspaces.id;



ALTER TABLE ONLY public.folders ALTER COLUMN id SET DEFAULT nextval('public.folders_id_seq'::regclass);



ALTER TABLE ONLY public.lists ALTER COLUMN id SET DEFAULT nextval('public.lists_id_seq'::regclass);



ALTER TABLE ONLY public.permissions ALTER COLUMN id SET DEFAULT nextval('public.permissions_id_seq'::regclass);



ALTER TABLE ONLY public.role_permissions ALTER COLUMN id SET DEFAULT nextval('public.role_permissions_id_seq'::regclass);



ALTER TABLE ONLY public.roles ALTER COLUMN id SET DEFAULT nextval('public.roles_id_seq'::regclass);



ALTER TABLE ONLY public.spaces ALTER COLUMN id SET DEFAULT nextval('public.spaces_id_seq'::regclass);



ALTER TABLE ONLY public.tags ALTER COLUMN id SET DEFAULT nextval('public.tags_id_seq'::regclass);



ALTER TABLE ONLY public.task_statuses ALTER COLUMN id SET DEFAULT nextval('public.task_statuses_id_seq'::regclass);



ALTER TABLE ONLY public.tasks ALTER COLUMN id SET DEFAULT nextval('public.tasks_id_seq'::regclass);



ALTER TABLE ONLY public.user_sessions ALTER COLUMN id SET DEFAULT nextval('public.user_sessions_id_seq'::regclass);



ALTER TABLE ONLY public.users ALTER COLUMN id SET DEFAULT nextval('public.users_id_seq'::regclass);



ALTER TABLE ONLY public.workspace_members ALTER COLUMN id SET DEFAULT nextval('public.workspace_members_id_seq'::regclass);



ALTER TABLE ONLY public.workspaces ALTER COLUMN id SET DEFAULT nextval('public.workspaces_id_seq'::regclass);



ALTER TABLE ONLY public.folders
    ADD CONSTRAINT folders_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.lists
    ADD CONSTRAINT lists_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.permissions
    ADD CONSTRAINT permissions_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.role_permissions
    ADD CONSTRAINT role_permissions_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.spaces
    ADD CONSTRAINT spaces_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.tags
    ADD CONSTRAINT tags_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.task_assignees
    ADD CONSTRAINT task_assignees_pkey PRIMARY KEY (task_id, user_id);



ALTER TABLE ONLY public.task_statuses
    ADD CONSTRAINT task_statuses_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.task_tags
    ADD CONSTRAINT task_tags_pkey PRIMARY KEY (task_id, tag_id);



ALTER TABLE ONLY public.tasks
    ADD CONSTRAINT tasks_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.user_sessions
    ADD CONSTRAINT uni_user_sessions_token_hash UNIQUE (token_hash);



ALTER TABLE ONLY public.user_contacts
    ADD CONSTRAINT user_contacts_pkey PRIMARY KEY (user_id, contact_id);



ALTER TABLE ONLY public.user_sessions
    ADD CONSTRAINT user_sessions_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.workspace_members
    ADD CONSTRAINT workspace_members_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.workspaces
    ADD CONSTRAINT workspaces_pkey PRIMARY KEY (id);



CREATE INDEX idx_folders_deleted_at ON public.folders USING btree (deleted_at);



CREATE INDEX idx_folders_parent_id ON public.folders USING btree (parent_id);



CREATE INDEX idx_folders_space_id ON public.folders USING btree (space_id);



CREATE INDEX idx_lists_deleted_at ON public.lists USING btree (deleted_at);



CREATE INDEX idx_lists_folder_id ON public.lists USING btree (folder_id);



CREATE INDEX idx_lists_parent_status_id ON public.lists USING btree (parent_status_id);



CREATE INDEX idx_lists_space_id ON public.lists USING btree (space_id);



CREATE UNIQUE INDEX idx_permissions_code ON public.permissions USING btree (code);



CREATE INDEX idx_permissions_deleted_at ON public.permissions USING btree (deleted_at);



CREATE UNIQUE INDEX idx_role_permission ON public.role_permissions USING btree (role_id, permission_id);



CREATE INDEX idx_role_permissions_deleted_at ON public.role_permissions USING btree (deleted_at);



CREATE INDEX idx_role_permissions_permission_id ON public.role_permissions USING btree (permission_id);



CREATE INDEX idx_role_permissions_role_id ON public.role_permissions USING btree (role_id);



CREATE INDEX idx_roles_deleted_at ON public.roles USING btree (deleted_at);



CREATE INDEX idx_roles_workspace_id ON public.roles USING btree (workspace_id);



CREATE UNIQUE INDEX idx_space_tag_name ON public.tags USING btree (name);



CREATE INDEX idx_spaces_deleted_at ON public.spaces USING btree (deleted_at);



CREATE INDEX idx_spaces_workspace_id ON public.spaces USING btree (workspace_id);



CREATE INDEX idx_tags_space_id ON public.tags USING btree (space_id);



CREATE INDEX idx_task_statuses_deleted_at ON public.task_statuses USING btree (deleted_at);



CREATE INDEX idx_task_statuses_list_id ON public.task_statuses USING btree (list_id);



CREATE INDEX idx_task_statuses_space_id ON public.task_statuses USING btree (space_id);



CREATE INDEX idx_tasks_deleted_at ON public.tasks USING btree (deleted_at);



CREATE INDEX idx_tasks_list_id ON public.tasks USING btree (list_id);



CREATE INDEX idx_tasks_space_id ON public.tasks USING btree (space_id);



CREATE INDEX idx_user_sessions_user_id ON public.user_sessions USING btree (user_id);



CREATE UNIQUE INDEX idx_users_email ON public.users USING btree (email);



CREATE UNIQUE INDEX idx_users_username ON public.users USING btree (username);



CREATE INDEX idx_workspace_members_deleted_at ON public.workspace_members USING btree (deleted_at);



CREATE INDEX idx_workspace_members_role_id ON public.workspace_members USING btree (role_id);



CREATE INDEX idx_workspace_members_user_id ON public.workspace_members USING btree (user_id);



CREATE INDEX idx_workspace_members_workspace_id ON public.workspace_members USING btree (workspace_id);



CREATE UNIQUE INDEX idx_workspace_user ON public.workspace_members USING btree (workspace_id, user_id);



CREATE INDEX idx_workspaces_deleted_at ON public.workspaces USING btree (deleted_at);



ALTER TABLE ONLY public.folders
    ADD CONSTRAINT fk_folders_children FOREIGN KEY (parent_id) REFERENCES public.folders(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.lists
    ADD CONSTRAINT fk_folders_lists FOREIGN KEY (folder_id) REFERENCES public.folders(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.task_statuses
    ADD CONSTRAINT fk_lists_statuses FOREIGN KEY (list_id) REFERENCES public.lists(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.tasks
    ADD CONSTRAINT fk_lists_tasks FOREIGN KEY (list_id) REFERENCES public.lists(id);



ALTER TABLE ONLY public.role_permissions
    ADD CONSTRAINT fk_role_permissions_permission FOREIGN KEY (permission_id) REFERENCES public.permissions(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.role_permissions
    ADD CONSTRAINT fk_roles_permissions FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.lists
    ADD CONSTRAINT fk_spaces_lists FOREIGN KEY (space_id) REFERENCES public.spaces(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.tags
    ADD CONSTRAINT fk_spaces_tags FOREIGN KEY (space_id) REFERENCES public.spaces(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.tasks
    ADD CONSTRAINT fk_spaces_tasks FOREIGN KEY (space_id) REFERENCES public.spaces(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.task_assignees
    ADD CONSTRAINT fk_task_assignees_task FOREIGN KEY (task_id) REFERENCES public.tasks(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.task_assignees
    ADD CONSTRAINT fk_task_assignees_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.task_tags
    ADD CONSTRAINT fk_task_tags_tag FOREIGN KEY (tag_id) REFERENCES public.tags(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.task_tags
    ADD CONSTRAINT fk_task_tags_task FOREIGN KEY (task_id) REFERENCES public.tasks(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.user_contacts
    ADD CONSTRAINT fk_user_contacts_contacts FOREIGN KEY (contact_id) REFERENCES public.users(id);



ALTER TABLE ONLY public.user_contacts
    ADD CONSTRAINT fk_user_contacts_user FOREIGN KEY (user_id) REFERENCES public.users(id);



ALTER TABLE ONLY public.user_sessions
    ADD CONSTRAINT fk_users_refresh_tokens FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.workspace_members
    ADD CONSTRAINT fk_workspace_members_role FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE RESTRICT;



ALTER TABLE ONLY public.workspace_members
    ADD CONSTRAINT fk_workspaces_members FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.roles
    ADD CONSTRAINT fk_workspaces_roles FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.spaces
    ADD CONSTRAINT fk_workspaces_spaces FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;




