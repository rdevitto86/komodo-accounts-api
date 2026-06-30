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
  WafWebAcl,
  MetricFilterAlarm,
} from 'komodo-forge-sdk-ts/cdk/constructs';

export const API_NAME = 'komodo-customer-api';
export const CONTAINER_NAME = 'customer-api';
export const PUBLIC_PORT = 7051;
export const PRIVATE_PORT = 7052;
export const VERSION = 'latest';
export const EVAL_RULES_PATH = '/app/config/validation_rules.yaml';

export interface CustomerEnvConfig extends EnvConfig {
  customersTable: string;
}

export const DEV_CONFIG: CustomerEnvConfig = {
  ...defaultDevConfig(),
  name: API_NAME,
  maxCapacity: 1,
  customersTable: 'komodo-customers-dev',
  certificateArn: 'PLACEHOLDER-acm-cert-arn-us-east-2',
  secretPath: `komodo/${ENV_DEV}/${CONTAINER_NAME}`,
  vpcTag: `komodo-${ENV_DEV}`,
  domainName: `customer-${ENV_DEV}.komodo.com`,
  tags: {
    ...defaultTags(),
    project: API_NAME,
    environment: ENV_DEV,
    dataClassification: 'pii',
  },
};

export const STG_CONFIG: CustomerEnvConfig = {
  ...defaultStgConfig(),
  name: API_NAME,
  customersTable: 'komodo-customers-stg',
  certificateArn: 'PLACEHOLDER-acm-cert-arn-us-east-2',
  cloudFrontCertificateArn: 'PLACEHOLDER-acm-cert-arn-us-east-1',
  secretPath: `komodo/${ENV_STAGING}/${CONTAINER_NAME}`,
  vpcTag: `komodo-${ENV_STAGING}`,
  domainName: `customer-${ENV_STAGING}.komodo.com`,
  tags: {
    ...defaultTags(),
    project: API_NAME,
    environment: ENV_STAGING,
    dataClassification: 'pii',
  },
};

export const PROD_CONFIG: CustomerEnvConfig = {
  ...defaultProdConfig(),
  name: API_NAME,
  customersTable: 'komodo-customers-prod',
  certificateArn: 'PLACEHOLDER-acm-cert-arn-us-east-2',
  cloudFrontCertificateArn: 'PLACEHOLDER-acm-cert-arn-us-east-1',
  secretPath: `komodo/${ENV_PROD}/${CONTAINER_NAME}`,
  vpcTag: `komodo-${ENV_PROD}`,
  domainName: 'customer.komodo.com',
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
  cfg: CustomerEnvConfig;
}

export interface CustomerService {
  alb: elbv2.ApplicationLoadBalancer;
  service: ecs.FargateService;
  taskRole: iam.IRole;
  securityGroup: ec2.SecurityGroup;
  cloudMapService: servicediscovery.Service;
}

export const buildCustomerService = (stack: cdk.Stack, { vpc, cluster, logGroup, cfg }: ServiceBuildContext): CustomerService => {
  const image = ecs.ContainerImage.fromEcrRepository(
    ecr.Repository.fromRepositoryName(stack, 'Repo', API_NAME),
    VERSION,
  );

  const executionRole = new iam.Role(stack, 'ExecutionRole', {
    assumedBy: new iam.ServicePrincipal('ecs-tasks.amazonaws.com'),
    managedPolicies: [iam.ManagedPolicy.fromAwsManagedPolicyName('service-role/AmazonECSTaskExecutionRolePolicy')],
  });

  const taskRole = new iam.Role(stack, 'TaskRole', {
    assumedBy: new iam.ServicePrincipal('ecs-tasks.amazonaws.com'),
  });

  taskRole.addToPolicy(new iam.PolicyStatement({
    actions: ['secretsmanager:GetSecretValue'],
    resources: [`arn:aws:secretsmanager:*:*:secret:${cfg.secretPath}*`],
  }));

  const taskDef = new ecs.FargateTaskDefinition(stack, 'TaskDef', {
    cpu: cfg.cpu,
    memoryLimitMiB: cfg.memory,
    taskRole,
    executionRole,
  });

  taskDef.addContainer('CustomerApi', {
    containerName: CONTAINER_NAME,
    image,
    portMappings: [
      { containerPort: PUBLIC_PORT, protocol: ecs.Protocol.TCP },
      { containerPort: PRIVATE_PORT, protocol: ecs.Protocol.TCP },
    ],
    logging: ecs.LogDrivers.awsLogs({ logGroup, streamPrefix: 'server' }),
    healthCheck: {
      command: ['CMD', '/komodo', '-healthcheck'],
      interval: cdk.Duration.seconds(30),
      timeout: cdk.Duration.seconds(5),
      retries: 3,
    },
    environment: {
      APP_NAME: API_NAME,
      PORT: `:${PUBLIC_PORT}`,
      PORT_PRIVATE: `:${PRIVATE_PORT}`,
      VERSION,
      AWS_REGION: cfg.regions[0].region,
      DYNAMODB_TABLE: cfg.customersTable,
      AWS_SECRET_PATH: cfg.secretPath ?? '',
      S3_AVATAR_BUCKET: `komodo-customer-avatars-${cfg.env}`,
    },
  });

  const albSg = new ec2.SecurityGroup(stack, 'AlbSg', { vpc, allowAllOutbound: true });
  albSg.addIngressRule(ec2.Peer.anyIpv4(), ec2.Port.tcp(443));
  albSg.addIngressRule(ec2.Peer.anyIpv4(), ec2.Port.tcp(80));

  const serviceSg = new ec2.SecurityGroup(stack, 'ServiceSg', { vpc, allowAllOutbound: true });
  serviceSg.addIngressRule(albSg, ec2.Port.tcp(PUBLIC_PORT));
  serviceSg.addIngressRule(ec2.Peer.ipv4(vpc.vpcCidrBlock), ec2.Port.tcp(PRIVATE_PORT));

  const service = new ecs.FargateService(stack, 'Service', {
    cluster,
    taskDefinition: taskDef,
    securityGroups: [serviceSg],
    serviceName: `${API_NAME}-${cfg.env}`,
    desiredCount: cfg.minCapacity,
    assignPublicIp: false,
  });

  const alb = new elbv2.ApplicationLoadBalancer(stack, 'Alb', {
    vpc,
    internetFacing: true,
    securityGroup: albSg,
    loadBalancerName: `${API_NAME}-${cfg.env}`,
  });

  alb.addListener('Http', {
    port: 80,
    defaultAction: elbv2.ListenerAction.redirect({ protocol: 'HTTPS', port: '443', permanent: true }),
  });

  const tg = new elbv2.ApplicationTargetGroup(stack, 'Tg', {
    vpc,
    port: PUBLIC_PORT,
    protocol: elbv2.ApplicationProtocol.HTTP,
    targets: [service],
    healthCheck: { path: '/health', healthyHttpCodes: '200' },
  });

  const httpsListener = alb.addListener('Https', {
    port: 443,
    certificates: [elbv2.ListenerCertificate.fromArn(cfg.certificateArn)],
    defaultAction: elbv2.ListenerAction.fixedResponse(404, { contentType: 'application/json', messageBody: '{"error":"not found"}' }),
  });

  const publicPaths = ['/health', '/health/ready', '/v1/me/*', '/v1/communications/unsubscribe', '/v1/users/exists'];
  publicPaths.forEach((path, i) => {
    httpsListener.addTargetGroups(`Rule${i}`, {
      targetGroups: [tg],
      priority: i + 1,
      conditions: [elbv2.ListenerCondition.pathPatterns([path])],
    });
  });

  const namespace = new servicediscovery.PrivateDnsNamespace(stack, 'Namespace', {
    name: 'komodo.internal',
    vpc,
    description: 'Internal service discovery for Komodo APIs',
  });

  const cloudMapService = new servicediscovery.Service(stack, 'CloudMapService', {
    namespace,
    name: 'customer-api',
    dnsRecordType: servicediscovery.DnsRecordType.A,
    dnsTtl: cdk.Duration.seconds(10),
  });

  service.associateCloudMapService({ service: cloudMapService, containerName: CONTAINER_NAME, containerPort: PRIVATE_PORT });

  const scaling = service.autoScaleTaskCount({ minCapacity: cfg.minCapacity, maxCapacity: cfg.maxCapacity });
  scaling.scaleOnCpuUtilization('CpuScaling', {
    targetUtilizationPercent: 60,
    scaleInCooldown: cdk.Duration.seconds(60),
    scaleOutCooldown: cdk.Duration.seconds(30),
  });
  scaling.scaleOnMemoryUtilization('MemScaling', {
    targetUtilizationPercent: 70,
    scaleInCooldown: cdk.Duration.seconds(60),
    scaleOutCooldown: cdk.Duration.seconds(30),
  });

  return { alb, service, taskRole: taskRole as iam.IRole, securityGroup: serviceSg, cloudMapService };
};

export const buildWaf = (stack: cdk.Stack, alb: elbv2.ApplicationLoadBalancer): WafWebAcl => {
  const waf = new WafWebAcl(stack, 'Waf', {
    metricPrefix: 'KomodoCustomerWaf',
    associateAlb: alb,
    managedRuleGroups: [
      { name: 'AWSManagedRulesCommonRuleSet' },
      { name: 'AWSManagedRulesKnownBadInputsRuleSet' },
    ],
    globalRateLimit: 2000,
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

export const buildCustomerAlarms = (stack: cdk.Stack, logGroup: logs.ILogGroup, alb: elbv2.ApplicationLoadBalancer) => {
  new MetricFilterAlarm(stack, 'User5xx', {
    logGroup,
    filterPattern: '{ $.status >= 500 }',
    metricNamespace: 'KomodoCustomer',
    metricName: 'Customer5xxCount',
    alarmName: 'Customer5xxAlarm',
    threshold: 10,
  });

  new MetricFilterAlarm(stack, 'UserNotFound', {
    logGroup,
    filterPattern: '{ $.status = 404 && $.path = "/v1/customers/*" }',
    metricNamespace: 'KomodoCustomer',
    metricName: 'CustomerNotFoundCount',
    alarmName: 'CustomerNotFoundAlarm',
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

export const buildCustomersTable = (stack: cdk.Stack, env: string, customersTable: string, taskRole: iam.IRole): dynamodb.Table => {
  const isProd = env !== 'dev';
  const table = new dynamodb.Table(stack, 'CustomersTable', {
    tableName: customersTable,
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
    nonKeyAttributes: ['customer_id'],
  });
  table.grantReadWriteData(taskRole);
  return table;
};

export const buildCustomerExportsBucket = (stack: cdk.Stack, env: string, taskRole: iam.IRole): s3.Bucket => {
  const isProd = env !== 'dev';
  const bucket = new s3.Bucket(stack, 'CustomerExports', {
    bucketName: `komodo-customer-exports-${env}`,
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
  const bucket = new s3.Bucket(stack, 'CustomerAvatars', {
    bucketName: `komodo-customer-avatars-${env}`,
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

export const buildStack = (stack: cdk.Stack, cfg: CustomerEnvConfig): void => {
  const logGroup = createLogGroup(stack)
    .setLogGroupName(`/ecs/${API_NAME}-${cfg.env}`)
    .setRetention(logs.RetentionDays.ONE_MONTH)
    .setRemovalPolicy(cdk.RemovalPolicy.DESTROY)
    .build();

  const vpc = ec2.Vpc.fromLookup(stack, 'Vpc', { tags: { Name: cfg.vpcTag } });
  const cluster = new ecs.Cluster(stack, 'Cluster', { vpc, clusterName: `${API_NAME}-${cfg.env}` });
  const ctx: ServiceBuildContext = { vpc, cluster, logGroup, cfg };
  const svc = buildCustomerService(stack, ctx);

  if (cfg.tags) {
    for (const [key, value] of Object.entries(cfg.tags)) {
      cdk.Tags.of(stack).add(key, value);
    }
  }

  const table = buildCustomersTable(stack, cfg.env, cfg.customersTable, svc.taskRole);
  buildCustomerExportsBucket(stack, cfg.env, svc.taskRole);
  buildAvatarsBucket(stack, cfg.env, svc.taskRole);

  new cdk.CfnOutput(stack, 'AlbDnsName', { value: svc.alb.loadBalancerDnsName });
  new cdk.CfnOutput(stack, 'ClusterName', { value: cluster.clusterName });
  new cdk.CfnOutput(stack, 'ServiceName', { value: svc.service.serviceName });
  new cdk.CfnOutput(stack, 'CloudMapServiceArn', { value: svc.cloudMapService.serviceArn });
  new cdk.CfnOutput(stack, 'ServiceSecurityGroupId', { value: svc.securityGroup.securityGroupId });
  new cdk.CfnOutput(stack, 'DomainName', { value: cfg.domainName });
  new cdk.CfnOutput(stack, 'CustomersTableName', { value: cfg.customersTable });
  new cdk.CfnOutput(stack, 'CustomersTableStreamArn', { value: table.tableStreamArn! });
  new cdk.CfnOutput(stack, 'AvatarsBucketName', { value: `komodo-customer-avatars-${cfg.env}` });

  if (cfg.env === 'dev') return;

  const waf = buildWaf(stack, svc.alb);
  buildCustomerAlarms(stack, logGroup, svc.alb);

  new cdk.CfnOutput(stack, 'WafWebAclArn', { value: waf.webAcl.attrArn });
};

export const createInfra = () => {
  try {
    const app = new cdk.App();
    const env = app.node.tryGetContext('env');
    if (!env) throw new Error('missing env variable');
    const cfg = env === 'dev' ? DEV_CONFIG : env === 'stg' ? STG_CONFIG : PROD_CONFIG;
    if (!cfg) throw new Error(`unknown environment ${env}, expected dev|stg|prod`);

    const region = app.node.tryGetContext('region') ?? 'us-east-2';
    const stack = new cdk.Stack(app, `CustomerApi-${region}-${env}`, {
      env: { account: process.env.CDK_DEFAULT_ACCOUNT, region },
    });
    buildStack(stack, cfg);
  } catch (err) {
    console.error('failed to create infrastructure:', err);
    process.exit(1);
  }
};

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  createInfra();
}
