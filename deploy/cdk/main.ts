import * as cdk from 'aws-cdk-lib';
import * as cloudwatch from 'aws-cdk-lib/aws-cloudwatch';
import * as dynamodb from 'aws-cdk-lib/aws-dynamodb';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as ecr from 'aws-cdk-lib/aws-ecr';
import * as ecs from 'aws-cdk-lib/aws-ecs';
import * as elbv2 from 'aws-cdk-lib/aws-elasticloadbalancingv2';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as logs from 'aws-cdk-lib/aws-logs';
import { fileURLToPath } from 'node:url';
import { ENV_DEV, ENV_STAGING, ENV_PROD } from 'komodo-forge-sdk-ts/cdk/constants';
import type { EnvConfig } from 'komodo-forge-sdk-ts/cdk/config';
import {
  defaultDevConfig,
  defaultStgConfig,
  defaultProdConfig,
  defaultTags,
} from 'komodo-forge-sdk-ts/cdk/config';
import { createLogGroup, createAlarm } from 'komodo-forge-sdk-ts/cdk/observability';
import {
  FargatePublicService,
  FargatePrivateService,
  WafWebAcl,
  MetricFilterAlarm,
} from 'komodo-forge-sdk-ts/cdk/constructs';

export const API_NAME = 'komodo-user-api';
export const CONTAINER_NAME = 'user-api';
export const PUBLIC_PORT = 7051;
export const PRIVATE_PORT = 7052;
export const PUBLIC_VERSION = 'latest';
export const PRIVATE_VERSION = 'latest';
export const EVAL_RULES_PATH = '/app/config/validation_rules.yaml';

export interface UserEnvConfig extends EnvConfig {
  usersTable: string;
}

export const DEV_CONFIG: UserEnvConfig = {
  ...defaultDevConfig(),
  name: API_NAME,
  maxCapacity: 1,
  usersTable: 'komodo-users-dev',
  certificateArn: 'PLACEHOLDER-acm-cert-arn-us-east-2',
  secretPath: `komodo/${ENV_DEV}/${CONTAINER_NAME}`,
  vpcTag: `komodo-${ENV_DEV}`,
  domainName: `user-${ENV_DEV}.komodo.com`,
  tags: {
    ...defaultTags(),
    project: API_NAME,
    environment: ENV_DEV,
    dataClassification: 'pii',
  },
};

export const STG_CONFIG: UserEnvConfig = {
  ...defaultStgConfig(),
  name: API_NAME,
  usersTable: 'komodo-users-stg',
  certificateArn: 'PLACEHOLDER-acm-cert-arn-us-east-2',
  cloudFrontCertificateArn: 'PLACEHOLDER-acm-cert-arn-us-east-1',
  secretPath: `komodo/${ENV_STAGING}/${CONTAINER_NAME}`,
  vpcTag: `komodo-${ENV_STAGING}`,
  domainName: `user-${ENV_STAGING}.komodo.com`,
  tags: {
    ...defaultTags(),
    project: API_NAME,
    environment: ENV_STAGING,
    dataClassification: 'pii',
  },
};

export const PROD_CONFIG: UserEnvConfig = {
  ...defaultProdConfig(),
  name: API_NAME,
  usersTable: 'komodo-users-prod',
  certificateArn: 'PLACEHOLDER-acm-cert-arn-us-east-2',
  cloudFrontCertificateArn: 'PLACEHOLDER-acm-cert-arn-us-east-1',
  secretPath: `komodo/${ENV_PROD}/${CONTAINER_NAME}`,
  vpcTag: `komodo-${ENV_PROD}`,
  domainName: 'user.komodo.com',
  tags: {
    ...defaultTags(),
    project: API_NAME,
    environment: ENV_PROD,
    dataClassification: 'pii',
  },
};

export interface ServiceBuildContext {
  vpc: ec2.IVpc;
  cluster: ecs.ICluster;
  logGroup: logs.ILogGroup;
  cfg: UserEnvConfig;
}

export const buildPublicContainer = (stack: cdk.Stack, { vpc, cluster, logGroup, cfg }: ServiceBuildContext): FargatePublicService =>
  new FargatePublicService(stack, 'Public', {
    vpc,
    cluster,
    logGroup,
    serviceName: `${API_NAME}-public-${cfg.env}`,
    image: ecs.ContainerImage.fromEcrRepository(
      ecr.Repository.fromRepositoryName(stack, 'PublicRepo', `${API_NAME}-public`),
      PUBLIC_VERSION,
    ),
    containerPort: PUBLIC_PORT,
    cpu: cfg.cpu,
    memoryLimitMiB: cfg.memory,
    desiredCount: cfg.minCapacity,
    minCapacity: cfg.minCapacity,
    maxCapacity: cfg.maxCapacity,
    certificateArn: cfg.certificateArn,
    secretPath: cfg.secretPath,
    streamPrefix: 'public',
    healthCheckCommand: ['CMD', '/komodo', '-healthcheck'],
    environment: {
      APP_NAME: API_NAME,
      PORT: `:${PUBLIC_PORT}`,
      VERSION: PUBLIC_VERSION,
      EVAL_RULES_PATH: EVAL_RULES_PATH,
      AWS_REGION: cfg.regions[0].region,
      DYNAMODB_TABLE: cfg.usersTable,
      AWS_SECRET_PATH: cfg.secretPath ?? '',
    },
  });

export const buildPrivateContainer = (stack: cdk.Stack, { vpc, cluster, logGroup, cfg }: ServiceBuildContext): FargatePrivateService =>
  new FargatePrivateService(stack, 'Private', {
    vpc,
    cluster,
    logGroup,
    serviceName: `${API_NAME}-private-${cfg.env}`,
    image: ecs.ContainerImage.fromEcrRepository(
      ecr.Repository.fromRepositoryName(stack, 'PrivateRepo', `${API_NAME}-private`),
      PRIVATE_VERSION,
    ),
    containerPort: PRIVATE_PORT,
    cpu: cfg.cpu,
    memoryLimitMiB: cfg.memory,
    desiredCount: cfg.minCapacity,
    minCapacity: cfg.minCapacity,
    maxCapacity: cfg.maxCapacity,
    secretPath: cfg.secretPath,
    streamPrefix: 'private',
    healthCheckCommand: ['CMD', '/komodo', '-healthcheck'],
    environment: {
      APP_NAME: `${API_NAME}-internal`,
      PORT_PRIVATE: `:${PRIVATE_PORT}`,
      VERSION: PRIVATE_VERSION,
      AWS_REGION: cfg.regions[0].region,
      DYNAMODB_TABLE: cfg.usersTable,
      AWS_SECRET_PATH: cfg.secretPath ?? '',
    },
  });

export const buildWaf = (stack: cdk.Stack, alb: elbv2.ApplicationLoadBalancer): WafWebAcl => new WafWebAcl(stack, 'Waf', {
  metricPrefix: 'KomodoUserWaf',
  associateAlb: alb,
  managedRuleGroups: [
    { name: 'AWSManagedRulesCommonRuleSet' },
    { name: 'AWSManagedRulesKnownBadInputsRuleSet' },
  ],
  globalRateLimit: 2000,
  rateLimitRules: [
    {
      name: 'ProfileRateLimit',
      limit: 200,
      pathPrefix: '/v1/profile/',
    },
    {
      name: 'AddressRateLimit',
      limit: 200,
      pathPrefix: '/v1/addresses/',
    },
  ],
});

export const buildUserAlarms = (stack: cdk.Stack, logGroup: logs.ILogGroup, alb: elbv2.ApplicationLoadBalancer) => {
  new MetricFilterAlarm(stack, 'User5xx', {
    logGroup,
    filterPattern: '{ $.status >= 500 }',
    metricNamespace: 'KomodoUser',
    metricName: 'User5xxCount',
    alarmName: 'User5xxAlarm',
    threshold: 10,
  });

  new MetricFilterAlarm(stack, 'UserNotFound', {
    logGroup,
    filterPattern: '{ $.status = 404 && $.path = "/v1/users/*" }',
    metricNamespace: 'KomodoUser',
    metricName: 'UserNotFoundCount',
    alarmName: 'UserNotFoundAlarm',
    threshold: 100,
  });

  createAlarm(stack, new cloudwatch.Metric({
    metricName: 'TargetResponseTime',
    namespace: 'AWS/ApplicationELB',
    dimensionsMap: { LoadBalancer: alb.loadBalancerArn },
    statistic: 'p99',
    period: cdk.Duration.seconds(60),
  }))
    .setAlarmName('LatencyP99Alarm')
    .setThreshold(0.5)
    .setEvaluationPeriods(2)
    .setComparisonOperator(cloudwatch.ComparisonOperator.GREATER_THAN_THRESHOLD)
    .setTreatMissingData(cloudwatch.TreatMissingData.NOT_BREACHING)
    .build();
};

export const buildUserDynamoDB = (stack: cdk.Stack, tableName: string, ...taskRoles: iam.IRole[]) => {
  const table = dynamodb.Table.fromTableName(stack, 'UsersTable', tableName);
  for (const role of taskRoles) {
    table.grantReadWriteData(role);
  }
};

export const buildStack = (stack: cdk.Stack, cfg: UserEnvConfig): void => {
  const logGroup = createLogGroup(stack)
    .setLogGroupName(`/ecs/${API_NAME}-${cfg.env}`)
    .setRetention(logs.RetentionDays.ONE_MONTH)
    .setRemovalPolicy(cdk.RemovalPolicy.DESTROY)
    .build();

  const vpc = ec2.Vpc.fromLookup(stack, 'Vpc', { tags: { Name: cfg.vpcTag } });
  const cluster = new ecs.Cluster(stack, 'Cluster', { vpc, clusterName: `${API_NAME}-${cfg.env}` });
  const ctx: ServiceBuildContext = { vpc, cluster, logGroup, cfg };
  const publicSvc = buildPublicContainer(stack, ctx);
  const privateSvc = buildPrivateContainer(stack, ctx);

  if (cfg.tags) {
    for (const [key, value] of Object.entries(cfg.tags)) {
      cdk.Tags.of(stack).add(key, value);
    }
  }

  buildUserDynamoDB(stack, cfg.usersTable, publicSvc.taskDefinition.taskRole, privateSvc.taskDefinition.taskRole);

  new cdk.CfnOutput(stack, 'AlbDnsName', { value: publicSvc.alb.loadBalancerDnsName });
  new cdk.CfnOutput(stack, 'ClusterName', { value: cluster.clusterName });
  new cdk.CfnOutput(stack, 'PublicServiceName', { value: publicSvc.service.serviceName });
  new cdk.CfnOutput(stack, 'PrivateServiceName', { value: privateSvc.service.serviceName });
  new cdk.CfnOutput(stack, 'DomainName', { value: cfg.domainName });
  new cdk.CfnOutput(stack, 'UsersTableName', { value: cfg.usersTable });

  if (cfg.env === 'dev') return;

  const waf = buildWaf(stack, publicSvc.alb);
  buildUserAlarms(stack, logGroup, publicSvc.alb);

  new cdk.CfnOutput(stack, 'WafWebAclArn', { value: waf.webAcl.attrArn });
};

export const createInfra = () => {
  try {
    const app = new cdk.App();
    const env = app.node.tryGetContext('env');
    if (!env) throw new Error('missing env variable');
    const cfg = env === 'dev' ? DEV_CONFIG : env === 'stg' ? STG_CONFIG : PROD_CONFIG;
    if (!cfg) throw new Error(`unknown environment ${env}, expected dev|stg|prod`);

    const account = cfg.account || app.node.tryGetContext('account') || '';

    for (const rd of cfg.regions) {
      if (!rd.enabled) continue;
      const suffix = rd.suffix ? `-${rd.suffix}` : '';
      const stack = new cdk.Stack(app, `KomodoUser-${cfg.env}${suffix}`, { env: { account, region: rd.region } });
      buildStack(stack, cfg);
    }
  } catch (err) {
    console.error('failed to create infrastructure:', err);
    process.exit(1);
  }
};

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  createInfra();
}
