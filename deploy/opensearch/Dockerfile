FROM opensearchproject/opensearch:3.1.0
RUN bin/opensearch-plugin install --batch analysis-icu && \
    bin/opensearch-plugin install --batch analysis-smartcn
RUN bin/opensearch-plugin install --batch repository-s3
