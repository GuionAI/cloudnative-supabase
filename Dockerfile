# Custom CNPG PostgreSQL with pgjwt extension for Supabase
FROM ghcr.io/cloudnative-pg/postgresql:16.4

USER root
RUN apt-get update && apt-get install -y make git gcc libc6-dev
RUN git clone https://github.com/michelp/pgjwt.git && \
    cd pgjwt && make install
RUN apt-get purge -y make git gcc libc6-dev && \
    apt-get autoremove -y && \
    rm -rf /var/lib/apt/lists/* pgjwt
USER 26
