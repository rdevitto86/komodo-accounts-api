import * as cdk from 'aws-cdk-lib';
import * as cloudwatch from 'aws-cdk-lib/aws-cloudwatch';
import * as dynamodb from 'aws-cdk-lib/aws-dynamodb';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as ecr from 'aws-cdk-lib/aws-ecr';
import * as ecs from 'aws-cdk-lib/aws-ecs';
import * as elbv2 from 'aws-cdk-lib/aws-elasticloadbalancingv2';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as logs from 'aws-cdk-lib/aws-logs';
import * as s3 from 'aws-cdk-lib/aws-s3';
import * as servicediscovery from 'aws-cdk-lib/aws-servicediscovery';
import { fileURLToPath } from 'node:url';
import type { EnvConfig } from 'komodo-forge-sdk-ts/aws/cdk/config';
import {
  defaultStgConfig,
  defaultProdConfig,
  defaultTags,
} from 'komodo-forge-sdk-ts/aws/cdk/config';
import {
  LogGroup,
  Alarm,
  WafWebAcl,
  MetricFilterAlarm,
  FargateService,
} from 'komodo-forge-sdk-ts/aws/cdk/constructs';
import { globalConstants } from 'komodo-forge-sdk-ts';
import { awsConstants } from 'komodo-forge-sdk-ts/aws';

export const API_NAME = process.env.APP_NAME || 'komodo-accounts-api';
export const CONTAINER_NAME = process.env.CONTAINER_NAME || 'accounts-api';
export const PUBLIC_PORT = parseInt(process.env.PUBLIC_PORT || '7051', 10);
export const PRIVATE_PORT = parseInt(process.env.PRIVATE_PORT || '7052', 10);
export const VERSION = process.env.VERSION || globalConstants.DEFAULT_APP_VERSION;
export const EVAL_RULES_PATH = process.env.EVAL_RULES_PATH || globalConstants.DEFAULT_EVAL_RULES_PATH;
export const HEALTH_CHECK_PATH = globalConstants.DEFAULT_HEALTH_CHECK_PATH;
export const HEALTH_CHECK_COMMAND = globalConstants.DEFAULT_HEALTH_CHECK_COMMAND;

const {
  DYNAMODB_TABLE_PREFIX,
  DYNAMODB_TABLE_SUFFIX_STG,
  DYNAMODB_TABLE_SUFFIX_PROD,
} = awsConstants.dynamodbConstants;

export interface AccountEnvConfig extends EnvConfig {
  accountsTable: string;
}

export const STG_CONFIG: AccountEnvConfig = {
  ...defaultStgConfig(),
  name: API_NAME,
  accountsTable: `${DYNAMODB_TABLE_PREFIX}accounts${DYNAMODB_TABLE_SUFFIX_STG}`,
  certificateArn: `PLACEHOLDER-acm-cert-arn-${awsConstants.REGION_EAST2}`,
  cloudFrontCertificateArn: `PLACEHOLDER-acm-cert-arn-${awsConstants.REGION_EAST1}`,
  secretPath: `${globalConstants.KOMODO_NAMESPACE}/${globalConstants.ENV_STAGING}/${CONTAINER_NAME}`,
  vpcTag: `${globalConstants.KOMODO_NAMESPACE}-${globalConstants.ENV_STAGING}`,
  domainName: `accounts-${globalConstants.ENV_STAGING}.${globalConstants.KOMODO_NAMESPACE}.com`,
  tags: {
    ...defaultTags(),
    project: API_NAME,
    environment: globalConstants.ENV_STAGING,
    dataClassification: 'pii',
  },
};

export const PROD_CONFIG: AccountEnvConfig = {
  ...defaultProdConfig(),
  name: API_NAME,
  accountsTable: `${DYNAMODB_TABLE_PREFIX}accounts${DYNAMODB_TABLE_SUFFIX_PROD}`,
  certificateArn: `PLACEHOLDER-acm-cert-arn-${awsConstants.REGION_EAST2}`,
  cloudFrontCertificateArn: `PLACEHOLDER-acm-cert-arn-${awsConstants.REGION_EAST1}`,
  secretPath: `${globalConstants.KOMODO_NAMESPACE}/${globalConstants.ENV_PROD}/${CONTAINER_NAME}`,
  vpcTag: `${globalConstants.KOMODO_NAMESPACE}-${globalConstants.ENV_PROD}`,
  domainName: `accounts.${globalConstants.KOMODO_NAMESPACE}.com`,
  tags: {
    ...defaultTags(),
    project: API_NAME,
    environment: globalConstants.ENV_PROD,
    dataClassification: 'pii',
  },
};

export interface ServiceBuildContext {
  vpc: ec2.IVpc;
  cluster: ecs.ICluster;
  logGroup: logs.ILogGroup;
  cfg: AccountEnvConfig;
}

export interface AccountService {
  alb: elbv2.ApplicationLoadBalancer;
  service: ecs.FargateService;
  taskDefinition: ecs.FargateTaskDefinition;
  taskRole: iam.IRole;
  securityGroup: ec2.SecurityGroup;
  cloudMapService: servicediscovery.Service;
}

export const buildAccountService = (stack: cdk.Stack, { vpc, cluster, logGroup, cfg }: ServiceBuildContext): AccountService => {
  const serviceName = `${API_NAME}-${cfg.env}`;

  const fargate = new FargateService(stack, 'Service', {
    vpc,
    cluster,
    logGroup,
    serviceName,
    image: ecs.ContainerImage.fromEcrRepository(
      ecr.Repository.fromRepositoryName(stack, 'Repo', API_NAME),
      VERSION,
    ),
    port: PUBLIC_PORT,
    privatePort: PRIVATE_PORT,
    cpu: cfg.cpu,
    memoryLimitMiB: cfg.memory,
    desiredCount: cfg.minCapacity,
    minCapacity: cfg.minCapacity,
    maxCapacity: cfg.maxCapacity,
    certificateArn: cfg.certificateArn,
    secretPath: cfg.secretPath,
    streamPrefix: 'server',
    healthCheckCommand: HEALTH_CHECK_COMMAND,
    healthCheckPath: HEALTH_CHECK_PATH,
    environment: {
      APP_NAME: API_NAME,
      PORT: `:${PUBLIC_PORT}`,
      PORT_PRIVATE: `:${PRIVATE_PORT}`,
      VERSION,
      EVAL_RULES_PATH,
      AWS_REGION: cfg.regions[0].region,
      DYNAMODB_TABLE: cfg.accountsTable,
      AWS_SECRET_PATH: cfg.secretPath ?? '',
      S3_AVATAR_BUCKET: `komodo-accounts-avatars-${cfg.env}`,
    },
  });

  const httpsListener = fargate.alb.node.findChild('HttpsListener') as elbv2.ApplicationListener;
  const cfnListener = httpsListener.node.defaultChild as elbv2.CfnListener;
  cfnListener.addPropertyOverride('DefaultActions', [{
    Type: 'fixed-response',
    FixedResponseConfig: {
      StatusCode: '404',
      ContentType: 'application/json',
      MessageBody: '{"error":"not found"}',
    },
  }]);

  const tg = new elbv2.ApplicationTargetGroup(stack, 'Tg', {
    vpc,
    port: PUBLIC_PORT,
    protocol: elbv2.ApplicationProtocol.HTTP,
    targets: [fargate.service],
    healthCheck: { path: HEALTH_CHECK_PATH, healthyHttpCodes: '200' },
  });

  const publicPaths = ['/health', '/health/ready', '/v1/me/*', '/v1/communications/unsubscribe', '/v1/accounts/exists'];
  for (const [i, path] of publicPaths.entries()) {
    httpsListener.addTargetGroups(`Rule${i}`, {
      targetGroups: [tg],
      priority: i + 1,
      conditions: [elbv2.ListenerCondition.pathPatterns([path])],
    });
  }

  const namespace = new servicediscovery.PrivateDnsNamespace(stack, 'Namespace', {
    name: 'komodo.internal',
    vpc,
    description: 'Internal service discovery for Komodo APIs',
  });

  const cloudMapService = new servicediscovery.Service(stack, 'CloudMapService', {
    namespace,
    name: 'accounts-api',
    dnsRecordType: servicediscovery.DnsRecordType.A,
    dnsTtl: cdk.Duration.seconds(10),
  });

  const container = fargate.taskDefinition.defaultContainer!;
  fargate.service.associateCloudMapService({ service: cloudMapService, container, containerPort: PRIVATE_PORT });

  return {
    alb: fargate.alb,
    service: fargate.service,
    taskDefinition: fargate.taskDefinition,
    taskRole: fargate.taskDefinition.taskRole,
    securityGroup: fargate.taskSecurityGroup,
    cloudMapService,
  };
};

export const buildWaf = (stack: cdk.Stack, alb: elbv2.ApplicationLoadBalancer): WafWebAcl => {
  const waf = new WafWebAcl(stack, 'Waf', {
    metricPrefix: 'KomodoAccountsWaf',
    associateAlb: alb,
    managedRuleGroups: [
      { name: awsConstants.WAF_MANAGED_RULE_COMMON },
      { name: awsConstants.WAF_MANAGED_RULE_KNOWN_BAD_INPUTS },
    ],
    globalRateLimit: awsConstants.WAF_DEFAULT_GLOBAL_RATE_LIMIT,
    rateLimitRules: [
      { name: 'ProfileRateLimit', limit: 200, pathPrefix: '/v1/profile/' },
      { name: 'AddressRateLimit', limit: 200, pathPrefix: '/v1/addresses/' },
    ],
  });

  waf.webAcl.addPropertyOverride('Rules.5', {
    Name: 'BlockInternalPaths',
    Priority: 6,
    Action: { Block: {} },
    Statement: {
      ByteMatchStatement: {
        SearchString: '/internal/',
        FieldToMatch: { UriPath: {} },
        TextTransformations: [{ Priority: 0, Type: 'NONE' }],
        PositionalConstraint: 'STARTS_WITH',
      },
    },
    VisibilityConfig: {
      SampledRequestsEnabled: true,
      CloudWatchMetricsEnabled: true,
      MetricName: 'BlockInternalPaths',
    },
  });
  return waf;
};

export const buildAccountAlarms = (stack: cdk.Stack, logGroup: logs.ILogGroup, alb: elbv2.ApplicationLoadBalancer) => {
  new MetricFilterAlarm(stack, 'Account5xx', {
    logGroup,
    filterPattern: '{ $.status >= 500 }',
    metricNamespace: 'KomodoAccounts',
    metricName: 'Account5xxCount',
    alarmName: 'Account5xxAlarm',
    threshold: 10,
  });

  new MetricFilterAlarm(stack, 'AccountNotFound', {
    logGroup,
    filterPattern: '{ $.status = 404 && $.path = "/v1/accounts/*" }',
    metricNamespace: 'KomodoAccounts',
    metricName: 'AccountNotFoundCount',
    alarmName: 'AccountNotFoundAlarm',
    threshold: 100,
  });

  new Alarm(stack, 'LatencyP99Alarm', {
    metric: new cloudwatch.Metric({
      metricName: awsConstants.cloudwatchConstants.METRIC_TARGET_RESPONSE_TIME,
      namespace: awsConstants.cloudwatchConstants.CLOUDWATCH_NAMESPACE_ALB,
      dimensionsMap: { LoadBalancer: alb.loadBalancerArn },
      statistic: 'p99',
      period: cdk.Duration.seconds(60),
    }),
    alarmName: 'LatencyP99Alarm',
    threshold: 0.5,
    evaluationPeriods: 2,
    comparisonOperator: cloudwatch.ComparisonOperator.GREATER_THAN_THRESHOLD,
    treatMissingData: cloudwatch.TreatMissingData.NOT_BREACHING,
  });
};

export const buildAccountsTable = (stack: cdk.Stack, env: string, accountsTable: string, taskRole: iam.IRole): dynamodb.Table => {
  const isProd = env !== 'dev';
  const table = new dynamodb.Table(stack, 'AccountsTable', {
    tableName: accountsTable,
    partitionKey: { name: 'PK', type: dynamodb.AttributeType.STRING },
    sortKey: { name: 'SK', type: dynamodb.AttributeType.STRING },
    billingMode: dynamodb.BillingMode.PAY_PER_REQUEST,
    stream: dynamodb.StreamViewType.NEW_AND_OLD_IMAGES,
    pointInTimeRecoverySpecification: { pointInTimeRecoveryEnabled: true },
    encryption: dynamodb.TableEncryption.AWS_MANAGED,
    deletionProtection: isProd,
    removalPolicy: isProd ? cdk.RemovalPolicy.RETAIN : cdk.RemovalPolicy.DESTROY,
  });
  table.addGlobalSecondaryIndex({
    indexName: 'GSI1',
    partitionKey: { name: 'GSI1PK', type: dynamodb.AttributeType.STRING },
    sortKey: { name: 'GSI1SK', type: dynamodb.AttributeType.STRING },
    projectionType: dynamodb.ProjectionType.INCLUDE,
    nonKeyAttributes: ['account_id'],
  });
  table.grantReadWriteData(taskRole);
  return table;
};

export const buildAccountExportsBucket = (stack: cdk.Stack, env: string, taskRole: iam.IRole): s3.Bucket => {
  const isProd = env !== 'dev';
  const bucket = new s3.Bucket(stack, 'AccountExports', {
    bucketName: `komodo-accounts-exports-${env}`,
    blockPublicAccess: s3.BlockPublicAccess.BLOCK_ALL,
    enforceSSL: true,
    encryption: s3.BucketEncryption.S3_MANAGED,
    versioned: false,
    removalPolicy: isProd ? cdk.RemovalPolicy.RETAIN : cdk.RemovalPolicy.DESTROY,
    autoDeleteObjects: !isProd,
    lifecycleRules: [{ expiration: cdk.Duration.days(7) }],
  });
  bucket.grantReadWrite(taskRole);
  return bucket;
};

export const buildAvatarsBucket = (stack: cdk.Stack, env: string, taskRole: iam.IRole): s3.Bucket => {
  const isProd = env !== 'dev';
  const bucket = new s3.Bucket(stack, 'AccountAvatars', {
    bucketName: `komodo-accounts-avatars-${env}`,
    blockPublicAccess: s3.BlockPublicAccess.BLOCK_ALL,
    enforceSSL: true,
    encryption: s3.BucketEncryption.S3_MANAGED,
    versioned: false,
    removalPolicy: isProd ? cdk.RemovalPolicy.RETAIN : cdk.RemovalPolicy.DESTROY,
    autoDeleteObjects: !isProd,
  });
  bucket.grantPut(taskRole);
  bucket.grantRead(taskRole);
  return bucket;
};

export const buildStack = (stack: cdk.Stack, cfg: AccountEnvConfig): void => {
  const logGroup = new LogGroup(stack, 'LogGroup', {
    logGroupName: `/ecs/${API_NAME}-${cfg.env}`,
    retention: logs.RetentionDays.ONE_MONTH,
    removalPolicy: cdk.RemovalPolicy.DESTROY,
  }).logGroup;

  const vpc = ec2.Vpc.fromLookup(stack, 'Vpc', { tags: { Name: cfg.vpcTag } });
  const cluster = new ecs.Cluster(stack, 'Cluster', { vpc, clusterName: `${API_NAME}-${cfg.env}` });
  const ctx: ServiceBuildContext = { vpc, cluster, logGroup, cfg };
  const svc = buildAccountService(stack, ctx);

  if (cfg.tags) {
    for (const [key, value] of Object.entries(cfg.tags)) {
      cdk.Tags.of(stack).add(key, value);
    }
  }

  const table = buildAccountsTable(stack, cfg.env, cfg.accountsTable, svc.taskRole);
  buildAccountExportsBucket(stack, cfg.env, svc.taskRole);
  buildAvatarsBucket(stack, cfg.env, svc.taskRole);

  new cdk.CfnOutput(stack, 'AlbDnsName', { value: svc.alb.loadBalancerDnsName });
  new cdk.CfnOutput(stack, 'ClusterName', { value: cluster.clusterName });
  new cdk.CfnOutput(stack, 'ServiceName', { value: svc.service.serviceName });
  new cdk.CfnOutput(stack, 'CloudMapServiceArn', { value: svc.cloudMapService.serviceArn });
  new cdk.CfnOutput(stack, 'ServiceSecurityGroupId', { value: svc.securityGroup.securityGroupId });
  new cdk.CfnOutput(stack, 'DomainName', { value: cfg.domainName });
  new cdk.CfnOutput(stack, 'AccountsTableName', { value: cfg.accountsTable });
  new cdk.CfnOutput(stack, 'AccountsTableStreamArn', { value: table.tableStreamArn! });
  new cdk.CfnOutput(stack, 'AvatarsBucketName', { value: `komodo-accounts-avatars-${cfg.env}` });

  const waf = buildWaf(stack, svc.alb);
  buildAccountAlarms(stack, logGroup, svc.alb);

  new cdk.CfnOutput(stack, 'WafWebAclArn', { value: waf.webAcl.attrArn });
};

export const createInfra = () => {
  try {
    const app = new cdk.App();
    const env = app.node.tryGetContext('env');
    if (env !== globalConstants.ENV_STAGING && env !== globalConstants.ENV_PROD) {
      throw new Error(`missing or invalid env context, expected stg|prod, got: ${env}`);
    }

    const cfg = env === globalConstants.ENV_STAGING ? STG_CONFIG : PROD_CONFIG;
    const account = cfg.account || app.node.tryGetContext('account') || '';
    const region = app.node.tryGetContext('region') ?? cfg.regions[0]?.region ?? awsConstants.REGION_EAST2;
    buildStack(new cdk.Stack(app, `AccountApi-${region}-${env}`, { env: { account, region } }), cfg);
  } catch (err) {
    console.error('failed to create infrastructure:', err);
    process.exit(1);
  }
};

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  createInfra();
}
